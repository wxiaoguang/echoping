package echoping

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

var ServerSessionTimeout = 3 * time.Second

type serverConnStat struct {
	pings      int64
	bytes      int64
	tempErrors int64
}

type serverConnSession struct {
	key string
	remoteAddr string
	lastActiveTime time.Time
	sessionId string

	udpChan chan []byte

	stat          serverConnStat
	lastStats     [2]serverConnStat
	lastStatTimes [2]time.Time
}

type Server struct {
	connSessions map[string]*serverConnSession
	mu sync.Mutex

	onceServerTimer sync.Once
}

func NewServer() *Server {
	s := &Server{
		connSessions: map[string]*serverConnSession{},
	}
	return s
}

func (server *Server) processEchoPingMessage(cs *serverConnSession, data []byte) (m map[string]interface{}, err error) {
	cs.lastActiveTime = time.Now()
	atomic.AddInt64(&cs.stat.pings, 1)
	atomic.AddInt64(&cs.stat.bytes, int64(len(data)))

	m = map[string]interface{}{}
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
		for _, cs := range server.connSessions {
			dur := now.Sub(cs.lastStatTimes[1])
			if cs.stat != cs.lastStats[1] || (dur < -d2s || d2s < dur) {
				cs.lastStats[0] = cs.lastStats[1]
				cs.lastStatTimes[0] = cs.lastStatTimes[1]
				cs.lastStats[1] = cs.stat
				cs.lastStatTimes[1] = now
			}
			if !cs.lastStatTimes[0].IsZero() {
				durSecs := cs.lastStatTimes[1].Sub(cs.lastStatTimes[0]).Seconds()

				// bps := float64(cs.lastStats[1].bytes - cs.lastStats[0].bytes) / durSecs
				pps := float64(cs.lastStats[1].pings- cs.lastStats[0].pings) / durSecs
				tmperr := cs.lastStats[1].tempErrors - cs.lastStats[0].tempErrors
				log.Printf("server stat %s (%s): pps=%.1f, tmperr=%d", cs.key, cs.sessionId, pps, tmperr)
			} else {
				log.Printf("server stat %s (%s): new connection", cs.key, cs.sessionId)
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
				if inactiveDur < -ServerSessionTimeout || ServerSessionTimeout < inactiveDur {
					log.Printf("server tcp conn %s session timeout", remoteAddr)
					break
				}
				if e.Temporary() {
					log.Printf("server tcp conn %s read error(temp): %v", remoteAddr, err)
					err = nil
					atomic.AddInt64(&cs.stat.tempErrors, 1)
					continue
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
			if e, ok := err.(net.Error); ok {
				if e.Temporary() {
					log.Printf("server tcp conn %s write error(temp): %v", remoteAddr, err)
					err = nil
					atomic.AddInt64(&cs.stat.tempErrors, 1)
					continue
				}
			}
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

func (server *Server) ServeEchoPingUdp(addr string) {
	server.onceServerTimer.Do(server.startServerTimer)

	for {
		var udpAddr *net.UDPAddr
		var udpConn *net.UDPConn
		var err error
		var n int

		udpAddr, err = net.ResolveUDPAddr("udp", addr)
		if err != nil {
			log.Printf("server udp resolve addr error: %v", err)
			goto onError
		}
		udpConn, err = net.ListenUDP("udp", udpAddr)
		if err != nil {
			log.Printf("server udp listen error: %v", err)
			goto onError
		}
		for {
			buf := make([]byte, 65536)
			oobBuf := make([]byte, 40)
			for {
				udpPacketInfo := UDPPacketInfo{}
				n, err = ReadMsgUDPacketInfo(udpConn, buf, oobBuf, &udpPacketInfo)
				if err != nil {
					if e, ok := err.(net.Error); ok {
						if e.Temporary() {
							log.Printf("server udp conn read error(temp): %v", err)
							continue
						}
					}
					log.Printf("server udp conn read error(serious): %v", err)
					goto onError
				}

				server.processEchoPingUdpPacket(udpConn, udpPacketInfo, buf[0:n])
			}
		}
	onError:
		if udpConn != nil {
			_ = udpConn.Close()
			udpConn = nil
		}
		time.Sleep(time.Second)
	}
}

func (server *Server) processEchoPingUdpPacket(conn *net.UDPConn, packetInfo UDPPacketInfo, data []byte) {
	remoteAddr := packetInfo.RemoteAddr().String()

	server.mu.Lock()
	csKey := "udp:" + remoteAddr
	cs := server.connSessions[csKey]
	if cs == nil {
		cs = &serverConnSession{}
		cs.key = csKey
		cs.remoteAddr = remoteAddr
		cs.lastActiveTime = time.Now()
		cs.udpChan = make(chan []byte, 100)
		server.connSessions[csKey] = cs

		log.Printf("server udp accept new conn: %s", remoteAddr)
		go server.handleEchoPingUdpConn(cs, conn, packetInfo)
	}
	server.mu.Unlock()
	cs.udpChan <- data
}

func (server *Server) handleEchoPingUdpConn(cs *serverConnSession, conn *net.UDPConn, packetInfo UDPPacketInfo) {
	var err error
	t1s := time.Tick(time.Second)
	loop:
	for {
		select {
			case data, ok := <- cs.udpChan:
				if !ok {
					break loop
				}
				if _, err = server.processEchoPingMessage(cs, data); err != nil {
					log.Printf("server udp conn %s message error(temp): %v", cs.remoteAddr, err)
					err = nil
					atomic.AddInt64(&cs.stat.tempErrors, 1)
					continue loop
				}
				if _, err = WriteMsgUDPPacketInfo(conn, data, &packetInfo); err != nil {
					if e, ok := err.(net.Error); ok {
						if e.Temporary() {
							log.Printf("server udp conn %s write error(temp): %v", cs.remoteAddr, err)
							err = nil
							atomic.AddInt64(&cs.stat.tempErrors, 1)
							continue loop
						}
					}
					log.Printf("server udp conn %s write error(serious): %v", cs.remoteAddr, err)
					break loop
				}
			case <- t1s:
				dur := time.Now().Sub(cs.lastActiveTime)
				if dur < -ServerSessionTimeout || ServerSessionTimeout < dur {
					break loop
				}
		}
	}
	if err == nil {
		log.Printf("server udp conn %s stopped, no more ping requests", cs.remoteAddr)
	} else {
		log.Printf("server udp conn %s stopped broken: %v", cs.remoteAddr, err)
	}

	server.mu.Lock()
	delete(server.connSessions, cs.key)
	server.mu.Unlock()
}
