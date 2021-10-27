package echoping

import (
    "bytes"
    "encoding/binary"
    "net"
    "syscall"
)

// UDPPacketInfo holds the remote address and the associated
// out-of-band data.
type UDPPacketInfo struct {
    raddr   *net.UDPAddr
    context []byte
}

// RemoteAddr returns the remote network address.
func (s *UDPPacketInfo) RemoteAddr() *net.UDPAddr { return s.raddr }


// setUDPSocketOptions4 prepares the v4 socket for packet info.
func setUDPSocketOptions4(conn *net.UDPConn) error {
    file, err := conn.File()
    if err != nil {
        return err
    }
    if err := syscall.SetsockoptInt(int(file.Fd()), syscall.IPPROTO_IP, syscall.IP_PKTINFO, 1); err != nil {
        return err
    }
    return nil
}


// getUDPSocketOptions6Only return true if the socket is v6 only and false when it is v4/v6 combined (dualstack).
func getUDPSocketOptions6Only(conn *net.UDPConn) (bool, error) {
    file, err := conn.File()
    if err != nil {
        return false, err
    }
    // dual stack. See http://stackoverflow.com/questions/1618240/how-to-support-both-ipv4-and-ipv6-connections
    v6only, err := syscall.GetsockoptInt(int(file.Fd()), syscall.IPPROTO_IPV6, syscall.IPV6_V6ONLY)
    if err != nil {
        return false, err
    }
    return v6only == 1, nil
}

func getUDPSocketName(conn *net.UDPConn) (syscall.Sockaddr, error) {
    file, err := conn.File()
    if err != nil {
        return nil, err
    }
    return syscall.Getsockname(int(file.Fd()))
}

// setUDPSocketOptions sets the UDP socket options.
// This function is implemented on a per platform basis. See udp_*.go for more details
func setUDPSocketOptions(conn *net.UDPConn) error {
    sa, err := getUDPSocketName(conn)
    if err != nil {
        return err
    }
    switch sa.(type) {
    case *syscall.SockaddrInet6:
        v6only, err := getUDPSocketOptions6Only(conn)
        if err != nil {
            return err
        }
        err = setUDPSocketOptions6(conn)
        if err != nil {
            return err
        }
        if !v6only {
            err = setUDPSocketOptions4(conn)
            if err != nil {
                return err
            }
        }
    case *syscall.SockaddrInet4:
        err = setUDPSocketOptions4(conn)
    }
    return err
}

// ReadMsgUDPacketInfo acts just like net.UDPConn.ReadFrom(), but fill a packet info object instead of a net.UDPAddr.
func ReadMsgUDPacketInfo(conn *net.UDPConn, b []byte, oobBuf []byte, packetInfo *UDPPacketInfo) (int, error) {
    //oob := make.. ( []byte, 40)
    n, oobn, _, raddr, err := conn.ReadMsgUDP(b, oobBuf)
    if err != nil {
        return n, err
    }
    packetInfo.raddr = raddr
    packetInfo.context = oobBuf[:oobn]
    return n, err
}

// WriteMsgUDPPacketInfo acts just like net.UDPConn.WriteTo(), but uses a *ReadMsgUDPacketInfo instead of a net.Addr.
func WriteMsgUDPPacketInfo(conn *net.UDPConn, b []byte, packetInfo *UDPPacketInfo) (int, error) {
    n, _, err := conn.WriteMsgUDP(b, packetInfo.context, packetInfo.raddr)
    return n, err
}

func (s *UDPPacketInfo) GetIpiAddrIP() net.IP {
/*
struct in_pktinfo {
    unsigned int   ipi_ifindex;  // Interface index
        struct in_addr ipi_spec_dst; // Local address
        struct in_addr ipi_addr;     // Header Destination address
    };
 */
    oob := bytes.NewBuffer(s.context)

    msg := syscall.Cmsghdr{}
    _ = binary.Read(oob, binary.LittleEndian, &msg)

    if msg.Level == syscall.IPPROTO_IP && msg.Type == syscall.IP_PKTINFO {
        pktInfo := syscall.Inet4Pktinfo{}
        _ = binary.Read(oob, binary.LittleEndian, &pktInfo)
        return net.IPv4(pktInfo.Addr[0], pktInfo.Addr[1], pktInfo.Addr[2], pktInfo.Addr[3])
    } else if msg.Level == syscall.IPPROTO_IPV6 && msg.Type == syscall.IP_PKTINFO {
        pktInfo := syscall.Inet6Pktinfo{}
        _ = binary.Read(oob, binary.LittleEndian, &pktInfo)
        return net.IP(pktInfo.Addr[:])
    }
    return net.IPv4zero
}
