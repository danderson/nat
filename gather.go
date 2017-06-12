package nat

import (
	"bytes"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"net"
	"time"

	"github.com/chripell/nat/stun"
)

const stunTimeout = 5 * time.Second

var stunserver = flag.String("stunserver", "stun.l.google.com:19302",
	"STUN server to query for reflexive address")

var lanNets = []*net.IPNet{
	{net.IPv4(10, 0, 0, 0), net.CIDRMask(8, 32)},
	{net.IPv4(172, 16, 0, 0), net.CIDRMask(12, 32)},
	{net.IPv4(192, 168, 0, 0), net.CIDRMask(16, 32)},
	{net.ParseIP("fc00"), net.CIDRMask(7, 128)},
}

type candidate struct {
	Addr *net.UDPAddr
	Prio int64
}

func (c candidate) String() string {
	return fmt.Sprintf("%#x %v", c.Prio, c.Addr)
}

func (c candidate) Equal(c2 candidate) bool {
	return c.Addr.IP.Equal(c2.Addr.IP) && c.Addr.Port == c2.Addr.Port
}

func getReflexive(sock *net.UDPConn) (*net.UDPAddr, error) {
	sock.SetDeadline(time.Now().Add(stunTimeout))
	defer sock.SetDeadline(time.Time{})

	serverAddr, err := net.ResolveUDPAddr("udp", *stunserver)
	if err != nil {

		return nil, errors.New("Couldn't resolve STUN server")
	}

	var tid [12]byte
	if _, err = rand.Read(tid[:]); err != nil {
		return nil, err
	}

	request, err := stun.BindRequest(tid[:], nil, true, false)
	if err != nil {
		return nil, err
	}

	n, err := sock.WriteTo(request, serverAddr)
	if err != nil {
		return nil, err
	}
	if n < len(request) {
		return nil, err
	}

	var buf [1024]byte
	n, _, err = sock.ReadFromUDP(buf[:])
	if err != nil {
		return nil, err
	}

	packet, err := stun.ParsePacket(buf[:n], nil)
	if err != nil {
		return nil, err
	}

	if packet.Class != stun.ClassSuccess || packet.Method != stun.MethodBinding || packet.Addr == nil || !bytes.Equal(tid[:], packet.Tid[:]) {
		return nil, errors.New("No address provided by STUN server")
	}

	return packet.Addr, nil
}

func pruneDups(cs []candidate) []candidate {
	ret := make([]candidate, 0, len(cs))
	for _, c := range cs {
		unique := true
		for _, c2 := range ret {
			if c.Equal(c2) {
				unique = false
				break
			}
		}
		if unique {
			ret = append(ret, c)
		}
	}
	return ret
}

func setPriorities(c []candidate) {
	for i := range c {
		// Prefer LAN over public net.
		for _, lan := range lanNets {
			if lan.Contains(c[i].Addr.IP) {
				c[i].Prio |= 1 << 32
			}
		}
		// Uniquify each priority. Keep the lower bits clear for
		// computing pair priorities.
		c[i].Prio += int64(i) << 16
	}
}

func GatherCandidates(sock *net.UDPConn) ([]candidate, error) {
	laddr := sock.LocalAddr().(*net.UDPAddr)
	ret := []candidate{}
	switch {
	case laddr.IP.IsLoopback():
		return nil, errors.New("Connecting over loopback not supported")
	case laddr.IP.IsUnspecified():
		addrs, err := net.InterfaceAddrs()
		if err != nil {
			return nil, err
		}

		for _, addr := range addrs {
			ip, ok := addr.(*net.IPNet)
			if ok && ip.IP.IsGlobalUnicast() {
				ret = append(ret, candidate{&net.UDPAddr{ip.IP, laddr.Port, ""}, 0})
			}
		}
	default:
		ret = append(ret, candidate{laddr, 0})
	}

	// Get the reflexive address
	reflexive, err := getReflexive(sock)
	if err == nil {
		ret = append(ret, candidate{reflexive, 0})
	}

	setPriorities(ret)
	return ret, nil
}
