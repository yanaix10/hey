package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

type pendingSession struct {
	aliceConn   net.Conn
	aliceReader *bufio.Reader
}

var (
	mu       sync.Mutex
	daemons  = make(map[string]net.Conn)       // username -> heyd connection
	sessions = make(map[string]pendingSession) // sessionID -> waiting sender
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "4244"
	}

	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("Relay failed to start: %v", err)
	}
	log.Printf("hey-relay listening on :%s", port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go handleConn(conn)
	}
}

func handleConn(conn net.Conn) {
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		conn.Close()
		return
	}

	parts := strings.Fields(strings.TrimSpace(line))
	if len(parts) == 0 {
		conn.Close()
		return
	}

	switch parts[0] {
	case "REGISTER": // REGISTER <username>
		if len(parts) < 2 {
			conn.Close()
			return
		}
		username := parts[1]
		mu.Lock()
		if old, ok := daemons[username]; ok {
			old.Close()
		}
		daemons[username] = conn
		mu.Unlock()
		log.Printf("Registered user: %s", username)

		// Send heartbeat every 25s to keep NAT firewall holes open
		for {
			time.Sleep(25 * time.Second)
			if _, err := conn.Write([]byte("HEARTBEAT\n")); err != nil {
				mu.Lock()
				if daemons[username] == conn {
					delete(daemons, username)
				}
				mu.Unlock()
				conn.Close()
				log.Printf("Disconnected user: %s", username)
				return
			}
		}

	case "REQUEST": // REQUEST <target> <sender>
		if len(parts) < 3 {
			conn.Close()
			return
		}
		target, sender := parts[1], parts[2]

		mu.Lock()
		daemonConn, online := daemons[target]
		if !online {
			mu.Unlock()
			conn.Write([]byte{2}) // 2 = user offline
			conn.Close()
			return
		}

		b := make([]byte, 8)
		_, _ = rand.Read(b)
		sessionID := hex.EncodeToString(b)
		sessions[sessionID] = pendingSession{aliceConn: conn, aliceReader: reader}
		mu.Unlock()

		// Trigger notification on target's daemon
		_, err := fmt.Fprintf(daemonConn, "PING %s %s\n", sessionID, sender)
		if err != nil {
			conn.Write([]byte{2})
			conn.Close()
		}

	case "DECISION": // DECISION <sessionID> <accepted|rejected>
		defer conn.Close()
		if len(parts) < 3 {
			return
		}
		sessionID, status := parts[1], parts[2]

		mu.Lock()
		sess, ok := sessions[sessionID]
		if !ok {
			mu.Unlock()
			return
		}
		if status != "accepted" {
			delete(sessions, sessionID)
			mu.Unlock()
			sess.aliceConn.Write([]byte{0}) // 0 = rejected
			sess.aliceConn.Close()
			return
		}
		mu.Unlock()
		sess.aliceConn.Write([]byte{1}) // 1 = accepted, waiting for terminal spawn

	case "BRIDGE": // BRIDGE <sessionID>
		if len(parts) < 2 {
			conn.Close()
			return
		}
		sessionID := parts[1]

		mu.Lock()
		sess, ok := sessions[sessionID]
		if ok {
			delete(sessions, sessionID)
		}
		mu.Unlock()

		if !ok {
			conn.Close()
			return
		}
		// Signal Alice that Bob's spawned terminal has connected
		if _, err := sess.aliceConn.Write([]byte{1}); err != nil {
			conn.Close()
			sess.aliceConn.Close()
			return
		}

		log.Printf("[BRIDGE %s] Active between Alice (%s) and Bob (%s)", sessionID, sess.aliceConn.RemoteAddr(), conn.RemoteAddr())

		// Pipe bytes bidirectionally using safe user-space buffers
		var wg sync.WaitGroup
		wg.Add(2)

		pipe := func(label string, dst net.Conn, src io.Reader) {
			defer wg.Done()
			buf := make([]byte, 32*1024)
			for {
				n, err := src.Read(buf)
				if n > 0 {
					if _, werr := dst.Write(buf[:n]); werr != nil {
						log.Printf("[BRIDGE %s] %s write error: %v", sessionID, label, werr)
						break
					}
				}
				if err != nil {
					log.Printf("[BRIDGE %s] %s read closed/error: %v", sessionID, label, err)
					break
				}
			}
		}

		go pipe("Bob->Alice", sess.aliceConn, reader)
		go pipe("Alice->Bob", conn, sess.aliceReader)

		go func() {
			wg.Wait()
			conn.Close()
			sess.aliceConn.Close()
			log.Printf("[BRIDGE %s] Terminated", sessionID)
		}()
	}
}