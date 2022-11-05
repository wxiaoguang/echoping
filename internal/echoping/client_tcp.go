package echoping

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

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
