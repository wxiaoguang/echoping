package echoping

import (
    "errors"
    "net"
)

// setUDPSocketOptions6 prepares the v6 socket for sessions.
func setUDPSocketOptions6(conn *net.UDPConn) error {
    return errors.New("unimplemented setUDPSocketOptions6 for darwin")
}

