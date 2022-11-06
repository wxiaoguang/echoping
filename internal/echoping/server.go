package echoping

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const ServerSessionTimeout = 3 * time.Second
const ServerQuicProto = "echoping"

type serverConnStat struct {
	pings      int64
	bytes      int64
	tempErrors int64
}

type serverConnSession struct {
	key            string
	remoteAddr     string
	lastActiveTime time.Time
	sessionId      string

	udpChan chan []byte

	stat          serverConnStat
	lastStats     [2]serverConnStat
	lastStatTimes [2]time.Time
}

type Server struct {
	connSessions map[string]*serverConnSession
	mu           sync.Mutex

	onceServerTimer sync.Once
}

func NewServer() *Server {
	s := &Server{
		connSessions: map[string]*serverConnSession{},
	}
	return s
}

func (server *Server) processEchoPingMessage(cs *serverConnSession, data []byte) (m map[string]any, err error) {
	cs.lastActiveTime = time.Now()
	atomic.AddInt64(&cs.stat.pings, 1)
	atomic.AddInt64(&cs.stat.bytes, int64(len(data)))

	m = map[string]any{}
	if err = json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if sessionId, ok := m["sid"].(string); ok {
		cs.sessionId = sessionId
	}
	return m, nil
}

func (server *Server) startServerTimer() {
	reportStats := func() {
		server.mu.Lock()
		defer server.mu.Unlock()

		now := time.Now()
		d2s := 2 * time.Second

		statMessages := map[string]string{}
		var statKeys []string

		for _, cs := range server.connSessions {
			dur := now.Sub(cs.lastStatTimes[1])
			if cs.stat != cs.lastStats[1] || (dur < -d2s || d2s < dur) {
				cs.lastStats[0] = cs.lastStats[1]
				cs.lastStatTimes[0] = cs.lastStatTimes[1]
				cs.lastStats[1] = cs.stat
				cs.lastStatTimes[1] = now
			}
			var statMessage string
			if !cs.lastStatTimes[0].IsZero() {
				durSecs := cs.lastStatTimes[1].Sub(cs.lastStatTimes[0]).Seconds()

				// bps := float64(cs.lastStats[1].bytes - cs.lastStats[0].bytes) / durSecs
				pps := float64(cs.lastStats[1].pings-cs.lastStats[0].pings) / durSecs
				tmperr := cs.lastStats[1].tempErrors - cs.lastStats[0].tempErrors
				statMessage = fmt.Sprintf("server stat %s (%s): pps=%.1f, tmperr=%d", cs.key, cs.sessionId, pps, tmperr)
			} else {
				statMessage = fmt.Sprintf("server stat %s (%s): new connection", cs.key, cs.sessionId)
			}
			statMessages[cs.key] = statMessage
			statKeys = append(statKeys, cs.key)
		}

		sort.Strings(statKeys)
		for _, key := range statKeys {
			log.Println(statMessages[key])
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
