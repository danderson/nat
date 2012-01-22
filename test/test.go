package main

import (
	"code.google.com/p/bouncer"
	"code.google.com/p/nat"
	"fmt"
)

func main() {
	sideband, initiator, err := bouncer.Dial("http://natulte.net:4242/bounce/lolol")

	if err != nil {
		fmt.Println(err)
		return
	}

	conn, err := nat.Connect(sideband, initiator)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Printf("Got a nat-busted connection! %s to %s", conn.LocalAddr(), conn.RemoteAddr())
	conn.Close()
}
