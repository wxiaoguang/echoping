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
			log.Printf("server tcp conn %s set read deadline error(serious): %v", remoteAddr, err)
			break
		}
		if data, err = br.ReadBytes('\n'); err != nil {
			if e, ok := err.(net.Error); ok {
				inactiveDur := time.Now().Sub(cs.lastActiveTime)
				if inactiveDur < -ServerSessionTimeout || ServerSessionTimeout < inactiveDur || e.Timeout() {
					log.Printf("server tcp conn %s session timeout", remoteAddr)
				}
			}
			break
		}

		if _, err = server.processEchoPingMessage(cs, data); err != nil {
			log.Printf("server tcp conn %s message error(temp): %v", remoteAddr, err)
			err = nil
			atomic.AddInt64(&cs.stat.tempErrors, 1)
			continue
		}

		if err = conn.SetDeadline(time.Now().Add(ServerSessionTimeout)); err != nil {
			log.Printf("server tcp conn %s set write deadline error(serious): %v", remoteAddr, err)
			break
		}
		if _, err = conn.Write(data); err != nil {
			break
		}
	}

	if err == io.EOF {
		log.Printf("server tcp conn %s closed gracefully: %v", remoteAddr, err)
	} else {
		log.Printf("server tcp conn %s closed broken: %v", remoteAddr, err)
	}

	server.mu.Lock()
	delete(server.connSessions, cs.key)
	server.mu.Unlock()

	_ = conn.Close()
}
