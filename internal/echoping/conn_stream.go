package echoping

import "net"

type ConnStream interface {
	net.Conn
}
