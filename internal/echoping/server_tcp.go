package echoping

import (
	"log"
	"net"
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
			go server.handleServerStream("tcp:"+tcpConn.RemoteAddr().String(), tcpConn.RemoteAddr(), tcpConn)
		}
	onError:
		if ln != nil {
			_ = ln.Close()
			ln = nil
		}
		time.Sleep(time.Second)
	}
}
