package echoping

import (
	"math/rand"
	"net"
	"sync"
	"syscall"
	"time"
)

type UDPConnMuxItem struct {
	RemoteAddr net.Addr
	PacketData []byte
}

type UDPConnMuxChanQuic interface {
	SetReadBuffer(int) error
	SyscallConn() (syscall.RawConn, error)
}
type UDPConnMuxChan struct {
	conn *net.UDPConn

	muChanRecv sync.Mutex
	chanRecv   chan *UDPConnMuxItem

	muChanSend sync.Mutex
	chanSend   chan *UDPConnMuxItem

	muDeadline    sync.Mutex
	deadlineRead  time.Time
	deadlineWrite time.Time
}

func NewUDPConnMuxChan(conn *net.UDPConn, queueSize int) *UDPConnMuxChan {
	return &UDPConnMuxChan{
		conn:     conn,
		chanRecv: make(chan *UDPConnMuxItem, queueSize),
		chanSend: make(chan *UDPConnMuxItem, queueSize),
	}
}

var _ net.PacketConn = (*UDPConnMuxChan)(nil)
var _ UDPConnMuxChanQuic = (*UDPConnMuxChan)(nil) // required by quic module

type timeoutError struct{}

func (timeoutError) Error() string { return "operation timed out" }
func (timeoutError) Timeout() bool { return true }

var emptyChan = make(<-chan time.Time, 0)

func (c *UDPConnMuxChan) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	c.muDeadline.Lock()
	deadline := c.deadlineRead
	c.muDeadline.Unlock()

	deadlineChan := emptyChan
	if !deadline.IsZero() {
		deadlineChan = time.After(deadline.Sub(time.Now()))
	}

	c.muChanRecv.Lock()
	chanRecv := c.chanRecv
	c.muChanRecv.Unlock()

	if chanRecv == nil {
		return 0, nil, net.ErrClosed
	}

	select {
	case item, ok := <-chanRecv:
		if !ok {
			return 0, nil, net.ErrClosed
		}
		n = copy(p, item.PacketData)
		addr = item.RemoteAddr
		return n, addr, nil
	case <-deadlineChan:
		return 0, nil, timeoutError{}
	}
}

func (c *UDPConnMuxChan) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	c.muDeadline.Lock()
	deadline := c.deadlineRead
	c.muDeadline.Unlock()

	c.muChanSend.Lock()
	if c.chanSend != nil {
		item := &UDPConnMuxItem{
			RemoteAddr: addr,
			PacketData: make([]byte, len(p)),
		}
		copy(item.PacketData, p)

		deadlineChan := emptyChan
		if !deadline.IsZero() {
			deadlineChan = time.After(deadline.Sub(time.Now()))
		}

		select {
		case c.chanSend <- item:
			n = len(p)
			err = nil
		case <-deadlineChan:
			n = 0
			err = timeoutError{}
		}
	}
	c.muChanSend.Unlock()
	return n, err
}

func (c *UDPConnMuxChan) Close() error {
	c.muChanRecv.Lock()
	if c.chanRecv != nil {
		close(c.chanRecv)
		c.chanRecv = nil
	}
	c.muChanRecv.Unlock()

	c.muChanSend.Lock()
	if c.chanSend != nil {
		close(c.chanSend)
		c.chanSend = nil
	}
	c.muChanSend.Unlock()

	return nil
}

func (c *UDPConnMuxChan) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *UDPConnMuxChan) SetDeadline(t time.Time) error {
	c.muDeadline.Lock()
	c.deadlineRead = t
	c.deadlineWrite = t
	c.muDeadline.Unlock()
	return nil
}

func (c *UDPConnMuxChan) SetReadDeadline(t time.Time) error {
	c.muDeadline.Lock()
	c.deadlineRead = t
	c.muDeadline.Unlock()
	return nil
}

func (c *UDPConnMuxChan) SetWriteDeadline(t time.Time) error {
	c.muDeadline.Lock()
	c.deadlineWrite = t
	c.muDeadline.Unlock()
	return nil
}

func (c *UDPConnMuxChan) SetReadBuffer(n int) error {
	return c.conn.SetReadBuffer(n)
}

func (c *UDPConnMuxChan) SyscallConn() (syscall.RawConn, error) {
	return c.conn.SyscallConn()
}

func (c *UDPConnMuxChan) EnqueueRecv(remoteAddr net.Addr, p []byte) (err error) {
	c.muChanRecv.Lock()
	if c.chanRecv != nil {
		item := &UDPConnMuxItem{
			RemoteAddr: remoteAddr,
			PacketData: make([]byte, len(p)),
		}
		copy(item.PacketData, p)
		select {
		case c.chanRecv <- item:
		default:
			err = timeoutError{}
		}
	} else {
		err = net.ErrClosed
	}
	c.muChanRecv.Unlock()
	return err
}

func (c *UDPConnMuxChan) DequeueSend() (remoteAddr net.Addr, p []byte, err error) {
	c.muChanSend.Lock()
	chanSend := c.chanSend
	c.muChanSend.Unlock()

	if chanSend == nil {
		return nil, nil, net.ErrClosed
	}

	item, ok := <-chanSend
	if !ok {
		return nil, nil, net.ErrClosed
	}
	return item.RemoteAddr, item.PacketData, nil
}

func UDPConnMux(conn *net.UDPConn, queueSize int, lossRatioSend, lossRatioRecv float64) (connForPacket net.PacketConn, connForQuic net.PacketConn) {
	connPacket := NewUDPConnMuxChan(conn, queueSize)
	connQuic := NewUDPConnMuxChan(conn, queueSize)

	// TODO: error handling

	closeMuxChans := func() {
		_ = connPacket.Close()
		_ = connQuic.Close()
	}
	go func() {
		mprng := rand.New(rand.NewSource(time.Now().UnixNano()))
		for {
			p := make([]byte, 4096)
			n, addr, err := conn.ReadFrom(p)
			if n == 0 || err != nil {
				closeMuxChans()
				break
			}
			if mprng.Float64() < lossRatioRecv {
				continue
			}
			p = p[:n]
			if p[0] == '{' {
				_ = connPacket.EnqueueRecv(addr, p)
			} else {
				_ = connQuic.EnqueueRecv(addr, p)
			}
		}
	}()

	dequeueSend := func(c *UDPConnMuxChan) {
		mprng := rand.New(rand.NewSource(time.Now().UnixNano()))
		for {
			remoteAddr, payload, err := c.DequeueSend()
			if err != nil {
				_ = c.Close()
				break
			}
			if mprng.Float64() < lossRatioSend {
				continue
			}
			_, _ = conn.WriteTo(payload, remoteAddr)
		}
	}

	go dequeueSend(connPacket)
	go dequeueSend(connQuic)

	return connPacket, connQuic
}
