package nat

import (
	"net"
	"time"
)

type Conn struct {
	conn          *net.UDPConn
	local, remote net.Addr
}

func newConn(sock *net.UDPConn, local, remote net.Addr) *Conn {
	sock.SetDeadline(time.Time{})
	return &Conn{sock, local, remote}
}

func (c *Conn) Read(b []byte) (int, error) {
	for {
		n, addr, err := c.conn.ReadFrom(b)
		// Generic non-address related errors.
		if addr == nil && err != nil {
			return n, err
		}
		// Filter out anything not related to the address we care
		// about.
		if addr.Network() != c.remote.Network() || addr.String() != c.remote.String() {
			continue
		}
		return n, err
	}
	panic("unreachable")
}

func (c *Conn) Write(b []byte) (int, error) {
	return c.conn.WriteTo(b, c.remote)
}

func (c *Conn) Close() error {
	return c.conn.Close()
}

func (c *Conn) LocalAddr() net.Addr {
	return c.local
}

func (c *Conn) RemoteAddr() net.Addr {
	return c.remote
}

func (c *Conn) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}

func (c *Conn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

func (c *Conn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}
