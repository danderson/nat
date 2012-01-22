package main

import (
	"code.google.com/p/nat"
	"fmt"
	"net"
)

var ch = make(chan bool)

func RunNat(sideband net.Conn, initiator bool) {
	defer func() { ch <- true }()
	conn, err := nat.Connect(sideband, initiator)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("Got a NAT connection! %s to %s\n", conn.LocalAddr(), conn.RemoteAddr())
}

func main() {
	fmt.Println("Pipe")
	a, b := net.Pipe()

	fmt.Println("Go 1")
	go RunNat(a, true)
	fmt.Println("Go 2")
	go RunNat(b, false)
	fmt.Println("ch 1")
	<-ch
	fmt.Println("ch 2")
	<-ch
}
