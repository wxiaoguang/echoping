package echoping

import (
	"log"
	"net"
	"sync"
	"time"
)

func (client *Client) ConnectEchoPingUdp(scheme string, addr string, lossRatioSend, lossRatioRecv float64) {
	client.onceClientTimer.Do(client.startClientTimer)

	for {
		var conn *net.UDPConn
		var udpListenAddr, udpRemoteAddr *net.UDPAddr
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
		{
			connPacket, connQuic := UDPConnMux(conn, 1024, lossRatioSend, lossRatioRecv)

			if scheme == "udp" {
				client.handleClientUdpPacket(addr, udpRemoteAddr, connPacket)
			} else if scheme == "quic" {
				client.handleClientUdpQuic(addr, udpRemoteAddr, connQuic)
			} else {
				log.Fatalf("client udp conn %s unknown scheme: %s", addr, scheme)
			}
		}
	onError:
		if conn != nil {
			_ = conn.Close()
		}
		time.Sleep(time.Second)
	}
}

func (client *Client) handleClientUdpPacket(addr string, remoteAddr *net.UDPAddr, conn net.PacketConn) {
	cs := &clientConnSession{
		key:                "udp:" + addr,
		remoteAddr:         addr,
		pingRequestRecords: map[string]*clientPingRequestRecord{},
		sessionId:          generateSessionId(),
	}

	client.mu.Lock()
	client.connSessions[cs.key] = cs
	client.mu.Unlock()

	exitLoop := false
	msgMap := map[string]any{}

	cs.remoteAddr = addr
	msgMap["sid"] = cs.sessionId
	log.Printf("client udp conn send to remote addr: %s, sid=%s", cs.remoteAddr, cs.sessionId)

	wg := &sync.WaitGroup{}
	wg.Add(2)

	go func() {
	loop:
		for !exitLoop {
			pingTime := time.Now()
			data := client.preparePingRequest(pingTime, cs, msgMap)
			if _, err := conn.WriteTo(data, remoteAddr); err != nil {
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
			if n, _, err = conn.ReadFrom(buf); err != nil {
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
}
