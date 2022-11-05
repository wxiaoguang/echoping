package echoping

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"runtime"
	"syscall"
)

// TODO: golang.org/x/net seems too complex to be used here. There is no generic or interface for x/net's IPv4 and IPv6

// UDPPacketInfo holds the remote address and the associated out-of-band data.
type UDPPacketInfo struct {
	remoteAddr *net.UDPAddr
	oobData    []byte
}

// RemoteAddr returns the remote network address.
func (s *UDPPacketInfo) RemoteAddr() *net.UDPAddr {
	return s.remoteAddr
}

func (s *UDPPacketInfo) GetIpiDstAddrIP() net.IP {
	/*
	   struct in_pktinfo (ipv4) {
	      unsigned int   ipi_ifindex;  // Interface index
	      struct in_addr ipi_spec_dst; // Local address
	      struct in_addr ipi_addr;     // Header Destination address
	   };
	*/
	oob := bytes.NewBuffer(s.oobData)

	msg := syscall.Cmsghdr{}
	_ = binary.Read(oob, binary.LittleEndian, &msg)

	if msg.Level == syscall.IPPROTO_IP && msg.Type == syscall.IP_PKTINFO {
		pktInfo := syscall.Inet4Pktinfo{}
		_ = binary.Read(oob, binary.LittleEndian, &pktInfo)
		return net.IPv4(pktInfo.Addr[0], pktInfo.Addr[1], pktInfo.Addr[2], pktInfo.Addr[3])
	} else if msg.Level == syscall.IPPROTO_IPV6 && msg.Type == syscall_IPV6_RECVPKTINFO {
		pktInfo := syscall.Inet6Pktinfo{}
		_ = binary.Read(oob, binary.LittleEndian, &pktInfo)
		return pktInfo.Addr[:]
	}
	return nil
}

type UDPConnWithInfo struct {
	*net.UDPConn
}

func NewUDPConnWithInfo(conn *net.UDPConn) (*UDPConnWithInfo, error) {
	err := enablePacketInfo(conn)
	if err != nil {
		return nil, err
	}
	return &UDPConnWithInfo{conn}, nil
}

// enablePacketInfo enables the packet info for the socket.
func enablePacketInfo(conn *net.UDPConn) error {
	file, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	errCtrl := file.Control(func(fdPtr uintptr) {
		fd := int(fdPtr)
		var sa syscall.Sockaddr
		sa, err = syscall.Getsockname(fd)
		if err != nil {
			err = fmt.Errorf("failed to getsockname: %w", err)
			return
		}

		var hasV4 bool

		if _, ok := sa.(*syscall.SockaddrInet6); ok {
			if syscall_IPV6_RECVPKTINFO == 0 {
				err = errors.New("unsupported OS for IPV6_RECVPKTINFO")
				return
			}
			err = syscall.SetsockoptInt(fd, syscall.IPPROTO_IPV6, int(syscall_IPV6_RECVPKTINFO), 1)
			if err != nil {
				err = fmt.Errorf("failed to SetsockoptInt IPPROTO_IPV6,IPV6_RECVPKTINFO: %w", err)
				return
			}
			// dual stack. See http://stackoverflow.com/questions/1618240/how-to-support-both-ipv4-and-ipv6-connections
			var v6only int
			v6only, err = syscall.GetsockoptInt(fd, syscall.IPPROTO_IPV6, syscall.IPV6_V6ONLY)
			if err != nil {
				err = fmt.Errorf("failed to GetsockoptInt IPPROTO_IPV6,IPV6_V6ONLY: %w", err)
				return
			}
			hasV4 = v6only == 0
		}

		if _, ok := sa.(*syscall.SockaddrInet4); ok || hasV4 {
			err = syscall.SetsockoptInt(fd, syscall.IPPROTO_IP, syscall.IP_PKTINFO, 1)
			// FIXME: is it necessary to set IP_PKTINFO on a IPv6 socket? it seems to cause an error on macOS.
			if err != nil && !hasV4 {
				err = fmt.Errorf("failed to SetsockoptInt IPPROTO_IP,IP_PKTINFO: %w", err)
				return
			}
			err = nil
		}
	})
	if errCtrl != nil {
		return fmt.Errorf("failed to control socket: %w", errCtrl)
	}
	return err
}

func (c *UDPConnWithInfo) ReadMsgUDPWithInfo(b []byte) (int, *UDPPacketInfo, error) {
	oobBuf := make([]byte, 40)
	n, oobn, _, raddr, err := c.ReadMsgUDP(b, oobBuf)
	if err != nil {
		return n, nil, err
	}
	packetInfo := &UDPPacketInfo{remoteAddr: raddr, oobData: oobBuf[:oobn]}
	return n, packetInfo, err
}

func (c *UDPConnWithInfo) WriteMsgUDPWithInfo(b []byte, packetInfo *UDPPacketInfo) (int, error) {
	n, _, err := c.WriteMsgUDP(b, packetInfo.oobData, packetInfo.remoteAddr)
	return n, err
}

var syscall_IPV6_RECVPKTINFO int32

func isOsDarwin() bool {
	//goland:noinspection GoBoolExpressions
	return runtime.GOOS == "darwin"
}

func isOsLinux() bool {
	//goland:noinspection GoBoolExpressions
	return runtime.GOOS == "linux"
}

func init() {
	if isOsDarwin() {
		// darwin #define IPV6_RECVPKTINFO        61
		syscall_IPV6_RECVPKTINFO = 61 //  no such const in Golang at the moment
	} else if isOsLinux() {
		// linux  #define IPV6_RECVPKTINFO        49
		syscall_IPV6_RECVPKTINFO = 49
	}
}
