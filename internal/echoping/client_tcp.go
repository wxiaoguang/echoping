package echoping

import (
	"log"
	"net"
	"time"
)

func (client *Client) ConnectEchoPingTcp(addr string) {
	client.onceClientTimer.Do(client.startClientTimer)

	for {
		conn, err := net.DialTimeout("tcp", addr, ClientPingTimeout)
		if err != nil {
			log.Printf("client tcp conn %s dial error: %s", addr, err)
			goto onError
		}

		client.handleClientStream("tcp:"+addr, conn.RemoteAddr(), conn)

	onError:
		if conn != nil {
			_ = conn.Close()
		}
		time.Sleep(time.Second)
	}
}
