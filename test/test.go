package main

import (
	"code.google.com/p/nat"
	"fmt"
	"net"
)

func main() {
	a, b := net.Pipe()

	go func() {
		fmt.Println(nat.Connect(a, false))
	}()
	fmt.Println(nat.Connect(b, true))
}
