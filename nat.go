package nat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/danderson/nat/stun"
)

type ExchangeCandidatesFun func([]byte) []byte

type Config struct {
	// ProbeTimeout is the duration between sending probes.
	ProbeTimeout time.Duration
	// DecisionTime is how much time we wait before checking (on the
	// initiator) all the links that successfully communicated (so they got
	// stun.ClassSuccess in response to a stun.ClassRequest) and deciding
	// which one to use.
	DecisionTime time.Duration
	// PeerDeadline is the duration for which the negotiation must go
	// on. Please note this must be > DecisionTime because after the
	// initiator decided which link to use, we need to have one more
	// complete round-trip (stun.ClassSuccess in response to a
	// stun.ClassRequest) but with UseCandidate set to true.
	PeerDeadline time.Duration
	// Prints all the ongoing handshakes.
	Verbose bool
	// Bind locally to a specific address.
	BindAddress *net.UDPAddr
	// Which interfaces use for ICE.
	UseInterfaces []string
	// Blacklist given addresses for ICE negotiation.
	BlacklistAddresses []*net.IPNet
	// TOS, if >0, sets IP_TOS to this value. Note an error is consider
	// non-fatal, if is just logged.
	TOS int
}

func DefaultConfig() *Config {
	return &Config{
		ProbeTimeout: 500 * time.Millisecond,
		DecisionTime: 4 * time.Second,
		PeerDeadline: 6 * time.Second,
		BindAddress:  &net.UDPAddr{},
		TOS:          -1,
	}
}

func ConnectOpt(xchg ExchangeCandidatesFun, initiator bool, cfg *Config) (net.Conn, error) {
	sock, err := net.ListenUDP("udp", cfg.BindAddress)
	if err != nil {
		return nil, err
	}
	if err := setTOS(sock, cfg.TOS); err != nil {
		log.Printf("Failed to set TOS to %d: %v", cfg.TOS, err)
	}

	engine := &attemptEngine{
		xchg:      xchg,
		sock:      sock,
		initiator: initiator,
		cfg:       cfg,
	}

	conn, err := engine.run()
	if err != nil {
		sock.Close()
		return nil, err
	}
	return conn, nil
}

func Connect(xchg ExchangeCandidatesFun, initiator bool) (net.Conn, error) {
	return ConnectOpt(xchg, initiator, DefaultConfig())
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
	p2pconn   net.Conn
	cfg       *Config
}

func (e *attemptEngine) init() error {
	candidates, err := GatherCandidates(e.sock, e.cfg.UseInterfaces, e.cfg.BlacklistAddresses)
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

	return nil
}

func (e *attemptEngine) xmit() (time.Time, error) {
	now := time.Now()
	var ret time.Time
	var err error

	for i := range e.attempts {
		if e.attempts[i].timeout.Before(now) {
			e.attempts[i].timeout = time.Now().Add(e.cfg.ProbeTimeout)
			e.attempts[i].tid, err = stun.RandomTid()
			if err != nil {
				return time.Time{}, err
			}
			packet, err := stun.BindRequest(e.attempts[i].tid, nil, false, e.attempts[i].chosen)
			if err != nil {
				return time.Time{}, err
			}
			if e.cfg.Verbose {
				log.Printf("TX probe %v to %v", e.attempts[i].tid, e.attempts[i].Addr)
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
		if e.cfg.Verbose {
			log.Printf("Cannot parse packet from %v: %v", from, err)
		}
		return nil
	}

	if packet.Method != stun.MethodBinding {
		if e.cfg.Verbose {
			log.Printf("Packet from %v is not a binding request", from)
		}
		return nil
	}

	switch packet.Class {
	case stun.ClassRequest:
		response, err := stun.BindResponse(packet.Tid[:], from, nil, false)
		if err != nil {
			if e.cfg.Verbose {
				log.Printf("Cannot bind response: %v", err)
			}
			return nil
		}
		e.sock.WriteToUDP(response, from)
		if e.cfg.Verbose {
			log.Printf("RX %v from %v use candidate %v, answering", packet.Tid[:], from, packet.UseCandidate)
		}
		if packet.UseCandidate {
			for i := range e.attempts {
				if from.String() != e.attempts[i].Addr.String() {
					continue
				}
				if !e.attempts[i].success {
					m := fmt.Errorf("bad link: local %v remote %v", e.attempts[i].localaddr, e.attempts[i].Addr)
					if e.cfg.Verbose {
						log.Printf("Error: %v", m)
					}
					return m
				}
				if e.p2pconn == nil {
					if e.cfg.Verbose {
						log.Printf("Confirmed local %v remote %v", e.attempts[i].localaddr, e.attempts[i].Addr)
					}
					e.p2pconn = newConn(e.sock, e.attempts[i].localaddr, e.attempts[i].Addr)
				}
				return nil
			}
		}

	case stun.ClassSuccess:
		if e.cfg.Verbose {
			log.Printf("RX %v from %v", packet.Tid[:], from)
		}
	skipAddress:
		for i := range e.attempts {
			if !bytes.Equal(packet.Tid[:], e.attempts[i].tid) {
				continue
			}
			if from.String() != e.attempts[i].Addr.String() {
				return nil
			}
			if e.attempts[i].chosen {
				if e.p2pconn == nil {
					if e.cfg.Verbose {
						log.Printf("Confirmed local %v remote %v", e.attempts[i].localaddr, e.attempts[i].Addr)
					}
					e.p2pconn = newConn(e.sock, e.attempts[i].localaddr, e.attempts[i].Addr)
					return nil
				}
			}
			for _, avoid := range e.cfg.BlacklistAddresses {
				if avoid.Contains(packet.Addr.IP) {
					continue skipAddress
				}
			}
			e.attempts[i].success = true
			e.attempts[i].localaddr = packet.Addr
			return nil
		}
	}

	return nil
}

func (e *attemptEngine) run() (net.Conn, error) {
	if err := e.init(); err != nil {
		return nil, err
	}

	endTime := time.Now().Add(e.cfg.PeerDeadline)
	decision := time.Now().Add(e.cfg.DecisionTime)

	for time.Now().Before(endTime) {
		if e.initiator && !decision.IsZero() && time.Now().After(decision) {
			decision = time.Time{}
			if err := e.decide(); err != nil {
				if e.cfg.Verbose {
					log.Printf("Decision failed: %v", err)
				}
				return nil, err
			}
		}

		timeout, err := e.xmit()
		if err != nil {
			if e.cfg.Verbose {
				log.Printf("TX failed: %v", err)
			}
			return nil, err
		}

		e.sock.SetReadDeadline(timeout)
		if err = e.read(); err != nil {
			if e.cfg.Verbose {
				log.Printf("RX failed: %v", err)
			}
			return nil, err
		}

	}

	if e.p2pconn != nil {
		if e.cfg.Verbose {
			log.Print("Success!")
		}
		return e.p2pconn, nil
	}
	for i := range e.attempts {
		if e.attempts[i].chosen {
			m := fmt.Errorf("last round with UseCandidate true failed even if a candidate circuit has been established.")
			if e.cfg.Verbose {
				log.Printf("Fail: %v", m)
			}
			return nil, m
		}
	}
	m := fmt.Errorf("no circuit could be established after %v", e.cfg.PeerDeadline)
	if e.cfg.Verbose {
		log.Printf("Fail: %v", m)
	}
	return nil, m
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
	if e.cfg.Verbose {
		log.Printf("Choosen: local %v remote %v", e.attempts[chosenpos].localaddr, e.attempts[chosenpos].Addr)
	}
	e.attempts[chosenpos].chosen = true
	e.attempts[chosenpos].timeout = time.Time{}
	return nil
}
