package echoping

import (
	"bufio"
	"io"
	"log"
	"net"
	"sync/atomic"
	"time"
)

func (server *Server) ServeEchoPingTcp(addr string) {
	server.onceServerTimer.Do(server.startServerTimer)

	for {
		var tcpAddr *net.TCPAddr
		var ln *net.TCPListener
		var err error

		tcpAddr, err = net.ResolveTCPAddr("tcp", addr)
		if err != nil {
			log.Printf("server tcp resolve addr error: %v", err)
			goto onError
		}
		ln, err = net.ListenTCP("tcp", tcpAddr)
		if err != nil {
			log.Printf("server tcp listen error: %v", err)
			goto onError
		}
		for {
			var tcpConn *net.TCPConn
			tcpConn, err = ln.AcceptTCP()
			if err != nil {
				log.Printf("server tcp accept error: %v", err)
				goto onError
			}

			log.Printf("server tcp accept new conn: %s", tcpConn.RemoteAddr().String())
			go server.handleEchoPingTcpConn(tcpConn)
		}
	onError:
		if ln != nil {
			_ = ln.Close()
			ln = nil
		}
		time.Sleep(time.Second)
	}
}

func (server *Server) handleEchoPingTcpConn(conn *net.TCPConn) {
	remoteAddr := conn.RemoteAddr().String()

	server.mu.Lock()
	csKey := "tcp:" + remoteAddr
	cs := server.connSessions[csKey]
	if cs == nil {
		cs = &serverConnSession{}
		cs.key = csKey
		cs.remoteAddr = remoteAddr
		cs.lastActiveTime = time.Now()
		server.connSessions[csKey] = cs
	}
	server.mu.Unlock()

	br := bufio.NewReader(conn)
	var err error
	var data []byte
	for {
		if err = conn.SetReadDeadline(time.Now().Add(ServerSessionTimeout)); err != nil {
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

		if err = conn.SetWriteDeadline(time.Now().Add(ServerSessionTimeout)); err != nil {
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
