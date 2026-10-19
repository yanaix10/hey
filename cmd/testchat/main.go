package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"
)

func main() {
	relayAddr := "104.214.168.159:4244"
	target := "target_slow"
	sender := "sender_slow"

	daemonConn, err := net.Dial("tcp", relayAddr)
	if err != nil {
		panic(err)
	}
	defer daemonConn.Close()
	fmt.Fprintf(daemonConn, "REGISTER %s\n", target)

	sessionChan := make(chan string, 1)
	go func() {
		r := bufio.NewReader(daemonConn)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			parts := strings.Fields(strings.TrimSpace(line))
			if len(parts) == 3 && parts[0] == "PING" {
				sessionID := parts[1]
				d, _ := net.Dial("tcp", relayAddr)
				fmt.Fprintf(d, "DECISION %s accepted\n", sessionID)
				d.Close()
				sessionChan <- sessionID
			}
		}
	}()

	aliceConn, err := net.Dial("tcp", relayAddr)
	if err != nil {
		panic(err)
	}
	defer aliceConn.Close()
	fmt.Fprintf(aliceConn, "REQUEST %s %s\n", target, sender)

	status := make([]byte, 1)
	io.ReadFull(aliceConn, status)
	sessionID := <-sessionChan

	bobConn, err := net.Dial("tcp", relayAddr)
	if err != nil {
		panic(err)
	}
	defer bobConn.Close()
	fmt.Fprintf(bobConn, "BRIDGE %s\n", sessionID)

	io.ReadFull(aliceConn, status)

	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := bobConn.Read(buf)
			if err != nil {
				log.Printf("[Bob] Read err: %v", err)
				return
			}
			log.Printf("[Bob] Read %d bytes: %q", n, buf[:n])
		}
	}()

	for i := 1; i <= 6; i++ {
		time.Sleep(1 * time.Second)
		log.Printf("[Alice] Sending Msg %d...", i)
		n, err := aliceConn.Write([]byte(fmt.Sprintf("Msg %d\n", i)))
		if err != nil {
			log.Printf("[Alice] Write err on Msg %d: %v", i, err)
			break
		}
		log.Printf("[Alice] Wrote %d bytes for Msg %d", n, i)
	}

	time.Sleep(2 * time.Second)
}
