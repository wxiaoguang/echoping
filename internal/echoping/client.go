package echoping

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"
)

var ClientPingTimeout = 3 * time.Second
var ClientPingInterval = 20 * time.Millisecond

type clientPingRequestRecord struct {
	sentTime time.Time
	sentBytes int64
	recvTime time.Time
	recvBytes int64
}

type clientConnSession struct {
	key string
	remoteAddr string
	sessionId string

	pingRequestRecords map[string]*clientPingRequestRecord
	mu sync.Mutex
}

type Client struct {
	connSessions map[string]*clientConnSession
	mu sync.Mutex

	onceClientTimer sync.Once
}

func NewClient() *Client {
	c := &Client{
		connSessions: map[string]*clientConnSession{},
	}
	return c
}

func (client *Client) startClientTimer() {
	d1s := 1 * time.Second
	d2s := 2 * time.Second

	reportStats := func() {
		client.mu.Lock()
		defer client.mu.Unlock()

		now := time.Now()

		// ping: round-trip min/avg/max/stddev = 9.993/15.272/20.551/5.279 ms
		for _, cs := range client.connSessions {
			cs.mu.Lock()
			loss := 0
			pingRoundTripDurations := make([]time.Duration, 0, len(cs.pingRequestRecords))

			var statsTimeBegin, statsTimeEnd time.Time
			for key, reqRec := range cs.pingRequestRecords {
				pingSentDur := now.Sub(reqRec.sentTime)
				if pingSentDur < -d2s || d2s < pingSentDur {
					// record timeout, should be removed
					delete(cs.pingRequestRecords, key)
				} else if d1s < pingSentDur && pingSentDur <= d2s {
					if statsTimeBegin.IsZero() || reqRec.sentTime.Before(statsTimeBegin) {
						statsTimeBegin = reqRec.sentTime
					}
					if statsTimeEnd.IsZero() || reqRec.sentTime.After(statsTimeEnd) {
						statsTimeEnd = reqRec.sentTime
					}
					if reqRec.recvTime.IsZero() {
						// packet lost, no response in 1 second
						loss++
					} else {
						pingRoundTripDurations = append(pingRoundTripDurations, reqRec.recvTime.Sub(reqRec.sentTime))
					}
				}
			}
			cs.mu.Unlock()
			if !statsTimeBegin.IsZero() && statsTimeBegin != statsTimeEnd {
				statsDurationSeconds := statsTimeEnd.Sub(statsTimeBegin).Seconds()
				pingRoundTripCount := len(pingRoundTripDurations)
				statsRequestCount := loss + pingRoundTripCount

				pps := float64(statsRequestCount) / statsDurationSeconds
				lossRatio := float64(loss/statsRequestCount)

				rttAvgMs := math.NaN()
				rttStddevMs := math.NaN()
				rttMinMs := math.NaN()
				rttMaxMs := math.NaN()
				rttP90Ms := math.NaN()

				if pingRoundTripCount > 0 {
					var rttSum, rttAvg, rttMin, rttMax time.Duration
					rttMin = -1
					rttMax = -1
					for _, v := range pingRoundTripDurations {
						rttSum += v
						if v < rttMin || rttMin == -1 {
							rttMin = v
						}
						if v > rttMax {
							rttMax = v
						}
					}
					rttAvg = rttSum / time.Duration(pingRoundTripCount)
					rttStddevMs = 0
					for _, v := range pingRoundTripDurations {
						d := (v - rttAvg).Seconds() * 1000
						rttStddevMs += d * d
					}
					rttStddevMs = math.Sqrt(rttStddevMs / float64(pingRoundTripCount))

					rttAvgMs = rttAvg.Seconds() * 1000
					rttMinMs = rttMin.Seconds() * 1000
					rttMaxMs = rttMax.Seconds() * 1000

					if pingRoundTripCount >= 10 {
						sort.SliceStable(pingRoundTripDurations, func(i, j int) bool {
							return pingRoundTripDurations[i] < pingRoundTripDurations[j]
						})
						p90idx := pingRoundTripCount*9/10
						rttP90Ms = pingRoundTripDurations[p90idx].Seconds() * 1000
					}
				}

				log.Printf("client stat %s (%s): pps=%.1f, loss=%.1f%%, round-trip time (ms): avg=%.1f, min=%.1f, max=%.1f, stddev=%.1f, p90=%.1f",
					cs.key, cs.sessionId,
					pps, lossRatio*100,
					rttAvgMs, rttMinMs, rttMaxMs, rttStddevMs, rttP90Ms)
			} else {
				log.Printf("client stat %s (%s) new connection", cs.key, cs.sessionId)
			}

		}
	}

	go func() {
		t1s := time.Tick(time.Second)
		for {
			select {
			case <-t1s:
				reportStats()
			}
		}
	}()
}

func (client *Client) preparePingRequest(t time.Time, cs *clientConnSession, msgMap map[string]interface{}) []byte {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	pingId := strconv.FormatInt(t.UnixNano(), 16)
	msgMap["pid"] = pingId
	data, _ := json.Marshal(msgMap)
	cs.pingRequestRecords[pingId] = &clientPingRequestRecord{sentTime: t, sentBytes: int64(len(data))}
	return data
}

func (client *Client) processPingResponse(t time.Time, cs *clientConnSession, data []byte) (map[string]interface{}, error) {
	msgMap := map[string]interface{}{}
	err := json.Unmarshal(data, &msgMap)
	if err != nil {
		return nil, err
	}

	pingId, _ := msgMap["pid"].(string)
	if pingId == "" {
		return nil, errors.New("broken ping response")
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()
	if reqRec, ok := cs.pingRequestRecords[pingId]; ok {
		reqRec.recvTime = t
		reqRec.recvBytes = int64(len(data))
	}

	return msgMap, nil
}

func (client *Client) ConnectEchoPingTcp(addr string) {
	client.onceClientTimer.Do(client.startClientTimer)

	cs := &clientConnSession{
		key:                "tcp:" + addr,
		remoteAddr:         addr,
		pingRequestRecords: map[string]*clientPingRequestRecord{},
	}

	client.mu.Lock()
	client.connSessions[cs.key] = cs
	client.mu.Unlock()

	for {
		var conn net.Conn
		var wg *sync.WaitGroup

		exitLoop := false
		msgMap := map[string]interface{}{}
		{
			var err error
			conn, err = net.DialTimeout("tcp", addr, ClientPingTimeout)
			if err != nil {
				log.Printf("client tcp conn %s dial error: %s", addr, err)
				goto onError
			}
			now := time.Now()
			cs.sessionId = fmt.Sprintf("%04d%02d%02d-%02d%02d%02d.%06d", now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(), now.Nanosecond()/1e3)
		}

		cs.remoteAddr = conn.RemoteAddr().String()
		msgMap["sid"] = cs.sessionId
		log.Printf("client tcp conn dialed: %s, sid=%s", conn.RemoteAddr().String(), cs.sessionId)

		wg = &sync.WaitGroup{}
		wg.Add(2)
		go func() {
			var err error
		loop:
			for !exitLoop {
				pingTime := time.Now()
				if err = conn.SetWriteDeadline(pingTime.Add(ClientPingTimeout)); err != nil {
					log.Printf("client tcp conn %s set write deadline error: %s", addr, err)
					break loop
				}

				data := client.preparePingRequest(pingTime, cs, msgMap)
				data = append(data, '\n')
				if _, err = conn.Write(data); err != nil {
					log.Printf("client tcp conn %s write error: %s", addr, err)
					break loop
				}

				elapsed := time.Now().Sub(pingTime)
				sleepDuration := ClientPingInterval - elapsed
				if sleepDuration > 0 {
					time.Sleep(ClientPingInterval)
				}
			}
			wg.Done()
		}()
		go func() {
			br := bufio.NewReader(conn)
			var err error
			var data []byte
		loop:
			for !exitLoop {
				if err = conn.SetReadDeadline(time.Now().Add(ClientPingTimeout)); err != nil {
					log.Printf("client tcp conn %s set read deadline error: %s", addr, err)
					break loop
				}
				if data, err = br.ReadBytes('\n'); err != nil {
					log.Printf("client tcp conn %s read error: %s", addr, err)
					break loop
				}
				_, err = client.processPingResponse(time.Now(), cs, data)
				if err != nil {
					log.Printf("client tcp conn %s message error: %s", addr, err)
					break loop
				}
			}
			wg.Done()
		}()
		wg.Wait()
	onError:
		if conn != nil {
			_ = conn.Close()
		}
		time.Sleep(time.Second)
	}
}

func (client *Client) ConnectEchoPingUdp(addr string) {
	client.onceClientTimer.Do(client.startClientTimer)

	cs := &clientConnSession{
		key:                "udp:" + addr,
		remoteAddr:         addr,
		pingRequestRecords: map[string]*clientPingRequestRecord{},
	}

	client.mu.Lock()
	client.connSessions[cs.key] = cs
	client.mu.Unlock()

	for {
		var conn *net.UDPConn
		var udpListenAddr, udpRemoteAddr *net.UDPAddr
		var wg *sync.WaitGroup

		exitLoop := false
		msgMap := map[string]interface{}{}
		{
			var err error
			if udpListenAddr, err = net.ResolveUDPAddr("udp", ":0"); err != nil {
				log.Printf("client udp conn %s resolve listen addr error: %s", addr, err)
				goto onError
			}
			if udpRemoteAddr, err = net.ResolveUDPAddr("udp", addr); err != nil {
				log.Printf("client udp conn %s resolve remote addr error: %s", addr, err)
				goto onError
			}

			conn, err = net.ListenUDP("udp", udpListenAddr)
			if err != nil {
				log.Printf("client udp conn %s listen error: %s", addr, err)
				goto onError
			}

			now := time.Now()
			cs.sessionId = fmt.Sprintf("%04d%02d%02d-%02d%02d%02d.%06d", now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(), now.Nanosecond()/1e3)
		}

		cs.remoteAddr = udpRemoteAddr.String()
		msgMap["sid"] = cs.sessionId
		log.Printf("client udp conn send to remote addr: %s, sid=%s", cs.remoteAddr, cs.sessionId)

		wg = &sync.WaitGroup{}
		wg.Add(2)

		go func() {
			loop:
			for !exitLoop {
				pingTime := time.Now()
				data := client.preparePingRequest(pingTime, cs, msgMap)
				if _, err := conn.WriteToUDP(data, udpRemoteAddr); err != nil {
					log.Printf("client udp conn %s write error: %s", addr, err)
					break loop
				}
				elapsed := time.Now().Sub(pingTime)
				sleepDuration := ClientPingInterval - elapsed
				if sleepDuration > 0 {
					time.Sleep(ClientPingInterval)
				}
			}
			exitLoop = true
			wg.Done()
		}()
		go func() {
			buf := make([]byte, 65536)
			var n int
			var err error
			loop:
			for !exitLoop {
				_ = conn.SetReadDeadline(time.Now().Add(time.Second))
				if n, _, err = conn.ReadFromUDP(buf); err != nil {
					if e, ok := err.(net.Error); ok {
						if e.Temporary() {
							continue
						}
					}
					log.Printf("client udp conn %s read error: %s", addr, err)
					break loop
				}
				data := buf[0:n]
				_, err = client.processPingResponse(time.Now(), cs, data)
				if err != nil {
					log.Printf("client udp conn %s message error: %s", addr, err)
					break loop
				}
			}
			exitLoop = true
			wg.Done()
		}()
		wg.Wait()
	onError:
		if conn != nil {
			_ = conn.Close()
		}
		time.Sleep(time.Second)
	}
}
