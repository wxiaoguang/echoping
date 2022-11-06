package echoping

import (
	"net"
	"time"
)

type UDPConnMuxChan struct {
}

var _ net.PacketConn = (*UDPConnMuxChan)(nil)

func (c *UDPConnMuxChan) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	//TODO implement me
	panic("implement me")
}

func (c *UDPConnMuxChan) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	//TODO implement me
	panic("implement me")
}

func (c *UDPConnMuxChan) Close() error {
	//TODO implement me
	panic("implement me")
}

func (c *UDPConnMuxChan) LocalAddr() net.Addr {
	//TODO implement me
	panic("implement me")
}

func (c *UDPConnMuxChan) SetDeadline(t time.Time) error {
	//TODO implement me
	panic("implement me")
}

func (c *UDPConnMuxChan) SetReadDeadline(t time.Time) error {
	//TODO implement me
	panic("implement me")
}

func (c *UDPConnMuxChan) SetWriteDeadline(t time.Time) error {
	//TODO implement me
	panic("implement me")
}
