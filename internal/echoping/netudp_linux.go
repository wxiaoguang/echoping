package echoping

import (
    "net"
    "syscall"
)

// setUDPSocketOptions6 prepares the v6 socket for sessions.
func setUDPSocketOptions6(conn *net.UDPConn) error {
    file, err := conn.File()
    if err != nil {
        return err
    }
    if err := syscall.SetsockoptInt(int(file.Fd()), syscall.IPPROTO_IPV6, syscall.IPV6_RECVPKTINFO, 1); err != nil {
        return err
    }
    return nil
}
