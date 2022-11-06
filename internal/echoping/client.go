package echoping

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"
)

var ClientPingTimeout = 3 * time.Second
var ClientPingInterval = 20 * time.Millisecond

type clientPingRequestRecord struct {
	sentTime  time.Time
	sentBytes int64
	recvTime  time.Time
	recvBytes int64
}

type clientConnSession struct {
	key        string
	remoteAddr string
	sessionId  string

	pingRequestRecords map[string]*clientPingRequestRecord
	mu                 sync.Mutex
}

type Client struct {
	connSessions map[string]*clientConnSession
	mu           sync.Mutex

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

		statMessages := map[string]string{}
		var statKeys []string

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

			var statMessage string
			if !statsTimeBegin.IsZero() && statsTimeBegin != statsTimeEnd {
				statsDurationSeconds := statsTimeEnd.Sub(statsTimeBegin).Seconds()
				pingRoundTripCount := len(pingRoundTripDurations)
				statsRequestCount := loss + pingRoundTripCount

				pps := float64(statsRequestCount) / statsDurationSeconds
				lossRatio := float64(loss) / float64(statsRequestCount)

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
						p90idx := pingRoundTripCount * 9 / 10
						rttP90Ms = pingRoundTripDurations[p90idx].Seconds() * 1000
					}
				}

				statMessage = fmt.Sprintf("client stat %s (%s): pps=%.1f, loss=%.1f%%, round-trip time (ms): avg=%.1f, min=%.1f, max=%.1f, stddev=%.1f, p90=%.1f",
					cs.key, cs.sessionId,
					pps, lossRatio*100,
					rttAvgMs, rttMinMs, rttMaxMs, rttStddevMs, rttP90Ms)
			} else {
				statMessage = fmt.Sprintf("client stat %s (%s) new connection", cs.key, cs.sessionId)
			}
			statKeys = append(statKeys, cs.key)
			statMessages[cs.key] = statMessage
		}

		sort.Strings(statKeys)
		for _, statKey := range statKeys {
			log.Println(statMessages[statKey])
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

func (client *Client) preparePingRequest(t time.Time, cs *clientConnSession, msgMap map[string]any) []byte {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	pingId := strconv.FormatInt(t.UnixNano(), 16)
	msgMap["pid"] = pingId
	data, _ := json.Marshal(msgMap)
	cs.pingRequestRecords[pingId] = &clientPingRequestRecord{sentTime: t, sentBytes: int64(len(data))}
	return data
}

func (client *Client) processPingResponse(t time.Time, cs *clientConnSession, data []byte) (map[string]any, error) {
	msgMap := map[string]any{}
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
