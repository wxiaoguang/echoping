package echoping

import (
	"context"
	"github.com/lucas-clemente/quic-go"
	_ "github.com/lucas-clemente/quic-go"
	"log"
	"time"
)

func (server *Server) ServeEchoPingQuic(c *UDPConnMuxChan) {
	for {
		var stream quic.Stream
		{
			listener, err := quic.Listen(c, generateTLSConfig(), nil)
			if err != nil {
				log.Printf("server quic listen error: %s", err)
				goto onError
			}

			conn, err := listener.Accept(context.Background())
			if err != nil {
				log.Printf("server quic accept error: %s", err)
				goto onError
			}
			stream, err = conn.AcceptStream(context.Background())
			if err != nil {
				log.Printf("server quic accept stream error: %s", err)
				goto onError
			}
			go server.handleServerStream("quic:"+conn.RemoteAddr().String(), conn.RemoteAddr(), stream)
		}
	onError:
		if stream != nil {
			_ = stream.Close()
		}
		time.Sleep(time.Second)
	}
}
