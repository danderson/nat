package nat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/danderson/nat/stun"
)

type ExchangeCandidatesFun func([]byte) []byte

func Connect(xchg ExchangeCandidatesFun, initiator bool) (net.Conn, error) {
	sock, err := net.ListenUDP("udp", &net.UDPAddr{})
	if err != nil {
		return nil, err
	}

	engine := &attemptEngine{
		xchg:      xchg,
		sock:      sock,
		initiator: initiator}

	conn, err := engine.run()
	if err != nil {
		sock.Close()
		return nil, err
	}
	return conn, nil
}

type attempt struct {
	candidate
	tid       []byte
	timeout   time.Time
	success   bool // did we get a STUN response from this addr
	chosen    bool // Has this channel been picked for the connection?
	localaddr net.Addr
}

type attemptEngine struct {
	xchg      ExchangeCandidatesFun
	sock      *net.UDPConn
	initiator bool
	attempts  []attempt
	decision  time.Time
	p2pconn   net.Conn
}

const (
	probeTimeout  = 500 * time.Millisecond
	probeInterval = 100 * time.Millisecond
	decisionTime  = 2 * time.Second
	peerDeadline  = 5 * time.Second
)

func (e *attemptEngine) init() error {
	candidates, err := GatherCandidates(e.sock)
	if err != nil {
		return err
	}

	var peerCandidates []candidate
	jsonCandidates, err := json.Marshal(candidates)
	if err != nil {
		panic(err)
	}
	err = json.Unmarshal(e.xchg(jsonCandidates), &peerCandidates)
	if err != nil {
		panic(err)
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
			packet, err := stun.BindRequest(e.attempts[i].tid, nil, false, e.attempts[i].chosen)
			if err != nil {
				return time.Time{}, err
			}
			e.sock.WriteToUDP(packet, e.attempts[i].Addr)
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
		e.sock.WriteToUDP(response, from)
		if packet.UseCandidate {
			for i := range e.attempts {
				if from.String() != e.attempts[i].Addr.String() {
					continue
				}
				if !e.attempts[i].success {
					return errors.New("Initiator told us to use bad link")
				}
				e.p2pconn = newConn(e.sock, e.attempts[i].localaddr, e.attempts[i].Addr)
				return nil
			}
		}

	case stun.ClassSuccess:
		for i := range e.attempts {
			if !bytes.Equal(packet.Tid[:], e.attempts[i].tid) {
				continue
			}
			if from.String() != e.attempts[i].Addr.String() {
				return nil
			}
			if e.attempts[i].chosen {
				e.p2pconn = newConn(e.sock, e.attempts[i].localaddr, e.attempts[i].Addr)
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
	if e.initiator {
		return
	}
	buf := new(bytes.Buffer)
	fmt.Fprintf(buf, "%t\t", e.initiator)
	for _, att := range e.attempts {
		timeout := att.timeout.Sub(time.Now())
		if timeout < 0 {
			timeout = 0
		}
		fmt.Fprintf(buf, "%s/%s/%s/%t\t", att.Addr, att.localaddr, timeout, att.success)
	}
	if e.initiator {
		buf.WriteString("\n")
	}
	fmt.Println(buf.String())
}

func (e *attemptEngine) run() (net.Conn, error) {
	if err := e.init(); err != nil {
		return nil, err
	}

	endTime := time.Now().Add(peerDeadline)
	for {
		if e.initiator && !e.decision.IsZero() && time.Now().After(e.decision) {
			e.decision = time.Time{}
			if err := e.decide(); err != nil {
				return nil, err
			}
		}

		e.debug()

		timeout, err := e.xmit()
		if err != nil {
			return nil, err
		}

		if time.Now().After(timeout) {
			timeout = time.Now().Add(peerDeadline)
		}

		e.sock.SetReadDeadline(timeout)
		if err = e.read(); err != nil {
			return nil, err
		}

		if e.p2pconn != nil {
			return e.p2pconn, nil
		}

		if time.Now().After(endTime) {
			return nil, fmt.Errorf("haven't heard from my peer after %v", peerDeadline)
		}
	}

	panic("unreachable")
}

func (e *attemptEngine) decide() error {
	var chosenpos int
	var chosenprio int64
	for i := range e.attempts {
		if e.attempts[i].success && e.attempts[i].Prio > chosenprio {
			chosenpos = i
			chosenprio = e.attempts[i].Prio
		}
	}
	if chosenprio == 0 {
		return errors.New("No feasible connection to peer")
	}

	// We need one final exchange over the chosen connection, to
	// indicate to the peer that we've picked this one. That's why we
	// expire whatever timeout there is here and now.
	e.attempts[chosenpos].chosen = true
	e.attempts[chosenpos].timeout = time.Time{}
	return nil
}
