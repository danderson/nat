package nat

import (
	"bytes"
	"code.google.com/p/nat/stun"
	"encoding/gob"
	"fmt"
	"net"
	"time"
)

func Connect(sideband net.Conn, initiator bool) (*net.UDPConn, error) {
	sock, err := net.ListenUDP("udp", &net.UDPAddr{})
	if err != nil {
		return nil, err
	}

	engine := &attemptEngine{
		sideband:  sideband,
		sock:      sock,
		initiator: initiator}

	err = engine.run()
	if err != nil {
		sock.Close()
		return nil, err
	}
	return sock, nil
}

type attempt struct {
	candidate
	tid       []byte
	timeout   time.Time
	success   bool // did we get a STUN response from this addr
	localaddr net.Addr
}

type attemptEngine struct {
	sideband  net.Conn
	sock      *net.UDPConn
	initiator bool
	attempts  []attempt
	decision  time.Time
}

const probeTimeout = 500 * time.Millisecond
const probeInterval = 100 * time.Millisecond
const decisionTime = 2 * time.Second

func (e *attemptEngine) init() error {
	candidates, err := GatherCandidates(e.sock)
	if err != nil {
		return err
	}

	encoder := gob.NewEncoder(e.sideband)
	decoder := gob.NewDecoder(e.sideband)
	var peerCandidates []candidate
	if e.initiator {
		if err = encoder.Encode(candidates); err != nil {
			return err
		}
		if err = decoder.Decode(&peerCandidates); err != nil {
			return err
		}
	} else {
		if err = decoder.Decode(&peerCandidates); err != nil {
			return err
		}
		if err = encoder.Encode(candidates); err != nil {
			return err
		}
	}

	e.attempts = make([]attempt, len(peerCandidates))
	for i := range peerCandidates {
		e.attempts[i].candidate = peerCandidates[i]
		e.attempts[i].timeout = time.Time{}
	}

	e.sock.SetWriteDeadline(time.Time{})
	e.decision = time.Now().Add(decisionTime)

	return nil
}

func (e *attemptEngine) xmit() (time.Time, error) {
	now := time.Now()
	var ret time.Time
	var err error

	for i := range e.attempts {
		if e.attempts[i].timeout.Before(now) {
			e.attempts[i].timeout = time.Now().Add(probeTimeout)
			e.attempts[i].tid, err = stun.RandomTid()
			if err != nil {
				return time.Time{}, err
			}
			packet, err := stun.BindRequest(e.attempts[i].tid, nil, false)
			if err != nil {
				return time.Time{}, err
			}
			_, err = e.sock.WriteToUDP(packet, e.attempts[i].Addr)
			if err != nil {
				return time.Time{}, err
			}
		}
		if ret.IsZero() || e.attempts[i].timeout.Before(ret) {
			ret = e.attempts[i].timeout
		}
	}
	return ret, nil
}

func (e *attemptEngine) read() error {
	buf := make([]byte, 512)
	n, from, err := e.sock.ReadFromUDP(buf)
	if err != nil {
		if neterr, ok := err.(net.Error); ok && neterr.Timeout() {
			return nil
		}
		return err
	}

	packet, err := stun.ParsePacket(buf[:n], nil)
	if err != nil {
		return nil
	}

	if packet.Method != stun.MethodBinding {
		return nil
	}

	switch packet.Class {
	case stun.ClassRequest:
		response, err := stun.BindResponse(packet.Tid[:], from, nil, false)
		if err != nil {
			return nil
		}
		_, err = e.sock.WriteToUDP(response, from)
		if err != nil {
			return err
		}

	case stun.ClassSuccess:
		for i := range e.attempts {
			if !bytes.Equal(packet.Tid[:], e.attempts[i].tid) {
				continue
			}
			if from.String() != e.attempts[i].Addr.String() {
				return nil
			}
			e.attempts[i].success = true
			e.attempts[i].localaddr = packet.Addr
			e.attempts[i].timeout = time.Now().Add(probeInterval)
			return nil
		}
	}

	return nil
}

func (e *attemptEngine) debug() {
	buf := new(bytes.Buffer)
	fmt.Fprintf(buf, "%t\t", e.initiator)
	for _, att := range e.attempts {
		fmt.Fprintf(buf, "%s/%s/%s/%t\t", att.Addr, att.localaddr, att.timeout.Sub(time.Now()), att.success)
	}
	if e.initiator {
		buf.WriteString("\n")
	}
	fmt.Println(buf.String())
}

func (e *attemptEngine) run() error {
	if err := e.init(); err != nil {
		return err
	}

	for {
		if time.Now().After(e.decision) {
			fmt.Println("Time to pick!")
			return nil
		}

		e.debug()

		timeout, err := e.xmit()
		if err != nil {
			return err
		}

		e.sock.SetReadDeadline(timeout)
		if err = e.read(); err != nil {
			return err
		}
	}

	return nil
}
