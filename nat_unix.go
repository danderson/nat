// +build !windows

package nat

import (
	"errors"
	"fmt"
	"net"
	"syscall"
)

func setTOS(conn net.Conn, tos int) error {
	if tos <= 0 {
		return nil
	}
	ipConn, ok := conn.(*net.IPConn)
	if !ok {
		return errors.New("Failed to set TOS, connection is not an IPConn")
	}
	f, err := ipConn.File()
	if err != nil {
		return fmt.Errorf("Failed to set TOS, connection retrieve the fd: %v", err)
	}
	defer f.Close()
	if err := syscall.SetsockoptInt(int(f.Fd()), syscall.SOL_SOCKET, syscall.IP_TOS, tos); err != nil {
		return fmt.Errorf("Failed to set TOS to %d: %v", tos, err)
	}
	return nil
}
