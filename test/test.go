package main

import (
	"code.google.com/p/gonat"
	"fmt"
	"net"
	"os"
)

func main() {
	sock, err := net.ListenUDP("udp", &net.UDPAddr{})
	if err != nil {
		fmt.Println("Couldn't listen on UDP port 4242")
		os.Exit(1)
	}

	fmt.Println(gonat.GatherCandidates(sock))
}
