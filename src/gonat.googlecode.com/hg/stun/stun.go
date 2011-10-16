package main

import (
	"encoding/binary"
	"os"
	"net"
	"log"
	"bytes"
	"io"
	"io/ioutil"
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

const magic = 0x2112a442

// mapped-address
// username
// message-integrity

type StunError struct {
	Class  int
	Number int
	Reason string
}

type StunResponse struct {
	MappedAddress      *net.UDPAddr
	MappedAddressXored bool
	Username           string
	Error              StunError
}

func readMappedAddr(attrValue io.Reader, xored bool, tid []byte) (*net.UDPAddr, os.Error) {
	var addr struct {
		Pad    byte
		Family uint8
		Port   uint16
	}
	if err := binary.Read(attrValue, binary.BigEndian, &addr); err != nil {
		return nil, err
	}
	ipdata, err := ioutil.ReadAll(attrValue)
	if err != nil {
		return nil, err
	}
	var expectedLen int
	switch addr.Family {
	case 1:
		expectedLen = 4
	case 2:
		expectedLen = 16
	default:
		return nil, os.NewError("Unknown address family")
	}
	if len(ipdata) != expectedLen {
		return nil, os.NewError("Bad IP length")
	}

	// If the attribute is a XOR mapped address, we need to decode it
	// first.
	if xored {
		addr.Port ^= 0x2112
		key := []byte{0x21, 0x12, 0xa4, 0x42}
		if len(ipdata) == 16 {
			key = append(key, tid...)
		}
		for i := range ipdata {
			ipdata[i] ^= key[i]
		}
	}

	return &net.UDPAddr{net.IP(ipdata).To16(), int(addr.Port)}, nil
}

func readAttrs(attrs io.Reader, resp *stunResponse, tid []byte) os.Error {
	for {
		var attrHeader struct {
			Type   uint16
			Length uint16
		}
		if err := binary.Read(attrs, binary.BigEndian, &attrHeader); err != nil {
			if err == os.EOF {
				break
			}
			return err
		}
		attrValue := io.LimitReader(attrs, int64(attrHeader.Length))
		log.Println("Attribute", attrHeader.Type)
		switch attrHeader.Type {
		case 0x1:
			// Some servers send back both XOR and non-XOR mapped
			// addresses. Skip unXORed ones if we have an address
			// already.
			if resp.mappedAddr == nil {
				maddr, err := readMappedAddr(attrValue, false, nil)
				if err != nil {
					return err
				}
				resp.mappedAddr = maddr
			}

		//case 0x8:
		case 0x20:
			maddr, err := readMappedAddr(attrValue, true, tid)
			if err != nil {
				return err
			}
			resp.mappedAddr = maddr

		case 0x8023:
			maddr, err := readMappedAddr(attrValue, false, nil)
			if err != nil {
				return err
			}
			log.Println("Alt server:", maddr)
		default:
			// Skip past the unknown attribute
			if _, err := ioutil.ReadAll(attrValue); err != nil {
				return err
			}
		}

		// Realign to 4-byte boundary
		if padding := (4 - (attrHeader.Length % 4)) % 4; padding != 0 {
			pad := make([]byte, padding)
			if _, err := io.ReadFull(attrs, pad); err != nil {
				return err
			}
		}
	}

	return nil
}

func readResponse(response []byte, expectedTid []byte) (*stunResponse, os.Error) {
	buf := bytes.NewBuffer(response)
	var header struct {
		ClassAndMethods uint16
		Length          uint16
		Magic           uint32
		Tid             [12]byte
	}
	if err := binary.Read(buf, binary.BigEndian, &header); err != nil {
		return nil, err
	}
	if header.Magic != magic {
		return nil, os.NewError("Bad magic in response")
	}
	if header.ClassAndMethods != (binding | success) {
		if header.ClassAndMethods == (binding | error) {
			return nil, os.NewError("STUN server returned binding error")
		} else {
			return nil, os.NewError("STUN server sent unexpected packet")
		}
	}
	if !bytes.Equal(header.Tid[:], expectedTid) {
		return nil, os.NewError("Wrong TID in response packet")
	}
	if header.Length == 0 {
		return nil, os.NewError("STUN response was empty")
	}

	// Keep track of the number of bytes of response to use when
	// computing the (potential) MAC, if we encounter a MAC attribute.
	//macLength := 20
	resp := &stunResponse{}

	if err := readAttrs(io.LimitReader(buf, int64(header.Length)), resp, expectedTid); err != nil {
		return nil, err
	}

	// We're done parsing attributes and apparently had no errors. If
	// we found a reflected address, we're good, else boom.
	if resp.mappedAddr == nil {
		return nil, os.NewError("No mapped address in STUN response")
	}
	return resp, nil
}

func RunStun(conn *net.UDPConn, server string) (*stunResponse, os.Error) {
	addr, err := net.ResolveUDPAddr("udp", server)
	if err != nil {
		return nil, err
	}

	request := []byte{
		0, 1, // Binding request
		0, 0, // Message length
		0x21, 0x12, 0xa4, 0x42, // magic
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12} // TID
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

	resp, err := readResponse(buf[:n], []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12})
	if err != nil {
		return nil, err
	}

	return resp, nil
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

	servers := []string{
		// "stun.l.google.com:19302",
		// "stun.ekiga.net:3478",
		// "stunserver.org:3478",
		// "stun.xten.com:3478",
		"numb.viagenie.ca:3478"}

	for _, server := range servers {
		log.Println("Running for", server)
		resp, err := RunStun(conn, server)
		log.Println(resp, err)
	}
}
