package echoping

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/lucas-clemente/quic-go"
	"net"
	"time"
)

func (client *Client) ConnectEchoPingQuic(c *UDPConnMuxChan, addr string) {
	client.onceClientTimer.Do(client.startClientTimer)

	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{ServerQuicProto},
	}

	for {
		var stream quic.Stream
		{
			ipAddr, err := net.ResolveIPAddr("udp", addr)
			if err != nil {
				fmt.Printf("client quic conn %s resolve error: %s", addr, err)
				goto onError
			}
			conn, err := quic.Dial(c, ipAddr, "echoping-host", tlsConf, nil)
			if err != nil {
				fmt.Printf("client quic conn %s dial error: %s", addr, err)
				goto onError
			}
			stream, err = conn.OpenStreamSync(context.Background())
			if err != nil {
				fmt.Printf("client quic conn %s open stream error: %s", addr, err)
				goto onError
			}

			client.handleClientStream("tcp:"+addr, conn.RemoteAddr(), stream)
		}
	onError:
		if stream != nil {
			_ = stream.Close()
		}
		time.Sleep(time.Second)
	}
}
