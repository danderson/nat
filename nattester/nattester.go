// nattester is a tool to quickly test ICE traversal when you have ssh working:
//
// scp nattester do.not.leak.hostnames.google.com:
// nattester --initiator=do.not.leak.hostnames.google.com

package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/danderson/nat"
)

var (
	initiator  = flag.String("initiator", "", "Name of host to ssh to")
	testString = flag.String("test_string",
		"The quick UDP packet jumped over the lazy TCP stream",
		"String to test echo on")
	cmd *exec.Cmd
)

func xchangeCandidates(mine []byte) []byte {
	var (
		out io.Reader
		err error
	)
	if *initiator == "" {
		fmt.Printf("%s\n", string(mine))
		out = os.Stdin
	} else {
		cmd = exec.Command("ssh", *initiator, "./nattester")
		cmd.Stdin = strings.NewReader(fmt.Sprintf("%s\n", mine))
		out, err = cmd.StdoutPipe()
		if err != nil {
			log.Fatal(err)
		}
		if cmd.Start() != nil {
			log.Fatal(err)
		}
	}
	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		ret := scanner.Bytes()
		if *initiator != "" {
			go func() {
				for scanner.Scan() {
					fmt.Printf("REMOTE: %s\n", string(scanner.Bytes()))
				}
			}()
		}
		return ret
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
	log.Fatal("Got no candidates")
	return nil
}

func main() {
	log.SetOutput(os.Stdout)
	flag.Parse()
	cfg := nat.DefaultConfig()
	cfg.Verbose = true
	conn, err := nat.ConnectOpt(xchangeCandidates, *initiator != "", cfg)
	if err != nil {
		log.Fatalf("NO CARRIER: %v\n", err)
	}
	log.Println("CONNECT 9600")
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	if *initiator == "" {
		// Poor man's echo sever
		io.Copy(conn, conn)
		return
	}
	ret := 1
	for {
		if _, err := conn.Write([]byte(*testString)); err != nil {
			log.Printf("NO CARRIER: %v\n", err)
			break
		}
		recv := make([]byte, len(*testString))
		if _, err := conn.Read(recv); err != nil {
			log.Printf("NO CARRIER: %v\n", err)
			break
		}
		log.Printf("RX: %v\n", recv)
		if bytes.Compare(recv, []byte(*testString)) == 0 {
			log.Print("Success!\n")
			ret = 0
			break
		}
	}
	cmd.Process.Kill()
	os.Exit(ret)
}
