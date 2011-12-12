package main

import (
	"fmt"
	"gonat.googlecode.com/hg/nat/stun"
	"io/ioutil"
	"net"
	"path/filepath"
)

func main() {
	paths, err := filepath.Glob("test-files/*")
	if err != nil {
		panic(err)
	}
	for _, path := range paths {
		data, err := ioutil.ReadFile(path)
		if err != nil {
			panic(err)
		}
		packet, err := stun.ParsePacket(data, []byte{})
		fmt.Println(packet, err)
	}
	pkt, err := stun.BindRequest([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 0, 1, 2}, []byte{1, 2, 3}, true)
	packet, err := stun.ParsePacket(pkt, []byte{1, 2, 3})
	fmt.Println(packet, err)

	pkt, err = stun.BindResponse([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 0, 1, 2}, &net.UDPAddr{net.IP([]byte{192, 168, 1, 42}), 4242}, []byte{1, 2, 3}, true)
	packet, err = stun.ParsePacket(pkt, []byte{1, 2, 3})
	fmt.Println(packet, err)
}
