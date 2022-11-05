package echoping

import (
	"log"
	"net"
	"sync/atomic"
	"time"
)

func (server *Server) ServeEchoPingUdp(addr string) {
	server.onceServerTimer.Do(server.startServerTimer)

	for {
		var udpAddr *net.UDPAddr
		var udpConnRaw *net.UDPConn
		var udpConn *UDPConnWithInfo
		var err error

		udpAddr, err = net.ResolveUDPAddr("udp", addr)
		if err != nil {
			log.Printf("server udp resolve addr error: %v", err)
			goto onError
		}
		udpConnRaw, err = net.ListenUDP("udp", udpAddr)
		if err != nil {
			log.Printf("server udp listen error: %v", err)
			goto onError
		}
		udpConn, err = NewUDPConnWithInfo(udpConnRaw)
		if err != nil {
			log.Printf("server udp init error: %v", err)
			goto onError
		}

		for {
			buf := make([]byte, 4096)
			for {
				n, packetInfo, err := udpConn.ReadMsgUDPWithInfo(buf)
				if err != nil {
					log.Printf("server udp conn read error(serious): %v", err)
					goto onError
				}

				server.processEchoPingUdpPacket(udpConn, packetInfo, buf[0:n])
			}
		}
	onError:
		if udpConnRaw != nil {
			_ = udpConnRaw.Close()
			udpConnRaw = nil
		}
		time.Sleep(time.Second)
	}
}

func (server *Server) processEchoPingUdpPacket(conn *UDPConnWithInfo, packetInfo *UDPPacketInfo, data []byte) {
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

func (server *Server) handleEchoPingUdpConn(cs *serverConnSession, conn *UDPConnWithInfo, packetInfo *UDPPacketInfo) {
	var err error
	t1s := time.Tick(time.Second)

loop:
	for {
		select {
		case data, ok := <-cs.udpChan:
			if !ok {
				break loop
			}
			if _, err = server.processEchoPingMessage(cs, data); err != nil {
				log.Printf("server udp conn %s message error(temp): %v", cs.remoteAddr, err)
				err = nil
				atomic.AddInt64(&cs.stat.tempErrors, 1)
				continue loop
			}
			if _, err = conn.WriteMsgUDPWithInfo(data, packetInfo); err != nil {
				log.Printf("server udp conn %s write error(serious): %v", cs.remoteAddr, err)
				break loop
			}
		case <-t1s:
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
