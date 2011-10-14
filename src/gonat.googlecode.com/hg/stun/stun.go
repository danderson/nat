package main

import (
	"encoding/binary"
	"os"
	"net"
	"log"
	"bytes"
	"io"
)

// Packet classes
const (
	request    = 0x0000
	indication = 0x0010
	success    = 0x0100
	error      = 0x0110
)

// Packet methods
const (
	binding = 0x0001
)

// mapped-address
// username
// message-integrity

//const stunServer = "stun.l.google.com:19302"
//const stunServer = "stun.ekiga.net:3478"
const stunServer = "stunserver.org:3478"

func readResponse(packet []byte) (payload []byte, os.Error) {
	resp := bytes.NewBuffer(packet)
	var header struct {
		Class uint16
		Length uint16
		Pad [16]byte
	}
	if err := binary.Read(resp, binary.BigEndian, &header); err != nil {
		return nil, nil, err
	}	

	if header.Class != 0x101 {
		return nil, nil, os.NewError("Unexpected response")
	}

	payload := make([]byte, header.Length)
	var n int
	if n, err = resp.Read(payload); err != nil {
		return nil, nil, err
	}
	if n != header.Length {
		return nil, nil, os.NewError("Short read")
	}
	return header.Tid, payload, nil
}
	

func RunStun(conn *net.UDPConn) (*net.UDPAddr, os.Error) {
	addr, err := net.ResolveUDPAddr("udp", stunServer)
	if err != nil {
		return nil, err
	}

	request := []byte{
		0, 1,  // Binding request
		0, 0,  // Message length
		0x21, 0x12, 0xa4, 0x42,  // magic
		1,2,3,4,5,6,7,8,9,10,11,12 }  // TID
	n, err := conn.WriteToUDP(request, addr)
	if err != nil {
		return nil, err
	}
	if n != len(request) {
		return nil, os.NewError("Failed to send request")
	}

	log.Println("Reading response")
	buf := make([]byte, 1024)
	n, _, err = conn.ReadFromUDP(buf)
	if err != nil {
		return nil, err
	}

	tid, payload, err := readResponse(buf[:n])
	if err != nil {
		return nil, err
	}

	var attrHeader struct {
		AttrType uint16
		Length uint16
	}

	if err = binary.Read(resp, binary.BigEndian, &attrHeader); err != nil {
		return nil, err
	}
	log.Printf("Attr Header: % #v", attrHeader)

	if attrHeader.AttrType == 1 {
		var mapped struct {
			Pad byte
			Family uint8
			Port uint16
		}
		
	}

	// log.Println(n)
	// log.Println(peer)
	// log.Printf("% x", resp[:n])

	return nil, os.NewError("prout")
}

func main() {
	addr, err := net.ResolveUDPAddr("udp", "0.0.0.0:4242")
	if err != nil {
		log.Fatalln(err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatalln(err)
	}
	res, err := RunStun(conn)
	log.Println(res)
	log.Println(err)
}
