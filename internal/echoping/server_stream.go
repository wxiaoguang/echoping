package echoping

import (
	"bufio"
	"io"
	"log"
	"net"
	"sync/atomic"
	"time"
)

func (server *Server) handleServerStream(sessionKey string, remoteAddr net.Addr, conn ConnStream) {

	server.mu.Lock()
	cs := server.connSessions[sessionKey]
	if cs == nil {
		cs = &serverConnSession{}
		cs.key = sessionKey
		cs.remoteAddr = remoteAddr.String()
		cs.lastActiveTime = time.Now()
		server.connSessions[sessionKey] = cs
	}
	server.mu.Unlock()

	br := bufio.NewReader(conn)
	var err error
	var data []byte
	for {
		if err = conn.SetDeadline(time.Now().Add(ServerSessionTimeout)); err != nil {
			log.Printf("server conn %s set read deadline error(serious): %v", sessionKey, err)
			break
		}
		if data, err = br.ReadBytes('\n'); err != nil {
			if e, ok := err.(net.Error); ok {
				inactiveDur := time.Now().Sub(cs.lastActiveTime)
				if inactiveDur < -ServerSessionTimeout || ServerSessionTimeout < inactiveDur || e.Timeout() {
					log.Printf("server conn %s session timeout", sessionKey)
				}
			}
			break
		}

		if _, err = server.processEchoPingMessage(cs, data); err != nil {
			log.Printf("server conn %s message error(temp): %v", sessionKey, err)
			err = nil
			atomic.AddInt64(&cs.stat.tempErrors, 1)
			continue
		}

		if err = conn.SetDeadline(time.Now().Add(ServerSessionTimeout)); err != nil {
			log.Printf("server conn %s set write deadline error(serious): %v", sessionKey, err)
			break
		}
		if _, err = conn.Write(data); err != nil {
			break
		}
	}

	if err == io.EOF {
		log.Printf("server conn %s closed gracefully: %v", sessionKey, err)
	} else {
		log.Printf("server conn %s closed broken: %v", sessionKey, err)
	}

	server.mu.Lock()
	delete(server.connSessions, cs.key)
	server.mu.Unlock()

	_ = conn.Close()
}
