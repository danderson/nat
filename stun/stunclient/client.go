// stunclient is a simple STUN client implementation using the STUN
// library.
package main

import (
	"flag"
	"fmt"
	"github.com/chripell/nat/stun"
	"net"
	"os"
	"time"
)

var sourcePort = flag.Int("srcport", 12345, "Source port to use for STUN request")
var server = flag.String("server", "stun.l.google.com:19302", "STUN server to query")

func main() {
	flag.Parse()
	serverAddr, err := net.ResolveUDPAddr("udp", *server)
	if err != nil {
		fmt.Println("Couldn't resolve", *server)
		os.Exit(1)
	}

	tid := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 0xa, 0xb, 0xc}
	request, err := stun.BindRequest(tid, nil, true, false)
	if err != nil {
		fmt.Println("Failed to build STUN request:", err)
		os.Exit(1)
	}

	sock, err := net.ListenUDP("udp", &net.UDPAddr{Port: *sourcePort})
	if err != nil {
		fmt.Println("Couldn't listen on UDP port", *sourcePort)
		os.Exit(1)
	}
	if err := sock.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		fmt.Println("Couldn't set the socket timeout:", err)
		os.Exit(1)
	}

	n, err := sock.WriteTo(request, serverAddr)
	if err != nil {
		fmt.Println("Couldn't send STUN request:", err)
		os.Exit(1)
	}
	if n < len(request) {
		fmt.Println("Short write")
		os.Exit(1)
	}

	buf := make([]byte, 1024)
	n, _, err = sock.ReadFromUDP(buf)
	if err != nil {
		fmt.Println("Error reading STUN response:", err)
		os.Exit(1)
	}
	sock.Close()

	packet, err := stun.ParsePacket(buf[:n], nil)
	if err != nil {
		fmt.Println("Failed to parse STUN packet:", err)
		os.Exit(1)
	}

	if packet.Error != nil {
		fmt.Println("STUN server returned an error:", packet.Error)
		os.Exit(1)
	}
	if packet.Addr == nil {
		fmt.Println("STUN server didn't provide a reflexive address")
		os.Exit(1)
	}

	fmt.Printf("According to STUN server %s, port %d maps to %s on your NAT\n",
		*server, *sourcePort, packet.Addr)
}
