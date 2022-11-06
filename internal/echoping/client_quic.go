package echoping

import (
	"context"
	"crypto/tls"
	"github.com/lucas-clemente/quic-go"
	"log"
	"net"
	"time"
)

func (client *Client) handleClientUdpQuic(addr string, remoteAddr *net.UDPAddr, udpConn net.PacketConn) {

	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{ServerQuicProto},
	}

	for {
		var stream quic.Stream
		{
			conn, err := quic.Dial(udpConn, remoteAddr, "echoping-host", tlsConf, nil)
			if err != nil {
				log.Printf("client quic conn %s dial error: %s", addr, err)
				goto onError
			}
			stream, err = conn.OpenStreamSync(context.Background())
			if err != nil {
				log.Printf("client quic conn %s open stream error: %s", addr, err)
				goto onError
			}

			client.handleClientStream("quic:"+addr, remoteAddr, stream)
		}
	onError:
		if stream != nil {
			_ = stream.Close()
		}
		time.Sleep(time.Second)
	}
}
