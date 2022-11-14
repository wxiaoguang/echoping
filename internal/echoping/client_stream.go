package echoping

import (
	"bufio"
	"log"
	"net"
	"sync"
	"time"
)

func (client *Client) handleClientStream(sessionKey string, remoteAddr net.Addr, conn ConnStream) {
	cs := &clientConnSession{
		key:                sessionKey,
		remoteAddr:         remoteAddr.String(),
		pingRequestRecords: map[string]*clientPingRequestRecord{},
		sessionId:          generateSessionId(),
	}

	client.mu.Lock()
	client.connSessions[cs.key] = cs
	client.mu.Unlock()

	var wg *sync.WaitGroup

	exitLoop := false
	msgMap := map[string]any{}
	msgMap["sid"] = cs.sessionId
	log.Printf("client stream conn dialed: %s, sid=%s", remoteAddr, cs.sessionId)

	wg = &sync.WaitGroup{}
	wg.Add(2)
	go func() {
		var err error
	loop:
		for !exitLoop {
			pingTime := time.Now()
			if err = conn.SetDeadline(pingTime.Add(ClientPingTimeout)); err != nil {
				log.Printf("client stream conn %s set write deadline error: %s", remoteAddr, err)
				break loop
			}

			data := client.preparePingRequest(pingTime, cs, msgMap)
			data = append(data, '\n')
			if _, err = conn.Write(data); err != nil {
				log.Printf("client stream conn %s write error: %s", remoteAddr, err)
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
			if err = conn.SetDeadline(time.Now().Add(ClientPingTimeout)); err != nil {
				log.Printf("client stream conn %s set read deadline error: %s", remoteAddr, err)
				break loop
			}
			if data, err = br.ReadBytes('\n'); err != nil {
				log.Printf("client stream conn %s read error: %s", remoteAddr, err)
				break loop
			}
			_, err = client.processPingResponse(time.Now(), cs, data)
			if err != nil {
				log.Printf("client stream conn %s message error: %s", remoteAddr, err)
				break loop
			}
		}
		wg.Done()
	}()
	wg.Wait()
}
