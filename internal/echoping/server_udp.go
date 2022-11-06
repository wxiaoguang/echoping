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
		var udpConn *net.UDPConn
		var err error

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

		{
			connPacket, connQuic := UDPConnMux(udpConn, 1024)
			go server.serveEchoPingUdpPacket(connPacket)
			go server.serveEchoPingUdpQuic(connQuic)
			select {} // TODO: better error handling
		}

	onError:
		if udpConn != nil {
			_ = udpConn.Close()
		}
		time.Sleep(time.Second)
	}
}

func (server *Server) serveEchoPingUdpPacket(conn net.PacketConn) {
	buf := make([]byte, 4096)

	// TODO: better error handling
	for {
		n, remoteAddr, err := conn.ReadFrom(buf)
		if err != nil {
			log.Printf("server udp conn read error(serious): %v", err)
			time.Sleep(time.Second)
			continue
		}

		server.mu.Lock()
		csKey := "udp:" + remoteAddr.String()
		cs := server.connSessions[csKey]
		if cs == nil {
			cs = &serverConnSession{}
			cs.key = csKey
			cs.remoteAddr = remoteAddr.String()
			cs.lastActiveTime = time.Now()
			cs.udpChan = make(chan []byte, 100)
			server.connSessions[csKey] = cs

			log.Printf("server udp accept new conn: %s", remoteAddr)
			go server.handleEchoPingUdpPacket(cs, conn, remoteAddr)
		}
		server.mu.Unlock()

		data := buf[:n]
		cs.udpChan <- data
	}
}

func (server *Server) handleEchoPingUdpPacket(cs *serverConnSession, conn net.PacketConn, remoteAddr net.Addr) {
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
			if _, err = conn.WriteTo(data, remoteAddr); err != nil {
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
