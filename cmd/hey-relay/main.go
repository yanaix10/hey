package main

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type pendingSession struct {
	aliceConn   net.Conn
	aliceReader *bufio.Reader
	createdAt   time.Time
}

type daemonEntry struct {
	conn net.Conn
	ip   string
}

var (
	mu       sync.Mutex
	daemons  = make(map[string]daemonEntry)
	sessions = make(map[string]pendingSession)
)

func generateTLSConfig() *tls.Config {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		log.Fatal(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{derBytes},
			PrivateKey:  priv,
		}},
	}
}

func remoteIP(conn net.Conn) string {
	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return conn.RemoteAddr().String()
	}
	return host
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "4244"
	}

	// Clean up abandoned pending sessions every 60s
	go func() {
		for {
			time.Sleep(60 * time.Second)
			mu.Lock()
			for id, s := range sessions {
				if time.Since(s.createdAt) > 2*time.Minute {
					s.aliceConn.Close()
					delete(sessions, id)
				}
			}
			mu.Unlock()
		}
	}()

	listener, err := tls.Listen("tcp", ":"+port, generateTLSConfig())
	if err != nil {
		log.Fatalf("Relay failed to start: %v", err)
	}
	log.Printf("hey-relay (TLS) listening on :%s", port)

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
	case "REGISTER":
		if len(parts) < 2 {
			conn.Close()
			return
		}
		baseUser := strings.ToLower(parts[1])
		clientIP := remoteIP(conn)
		assignedUser := baseUser

		mu.Lock()
		if existing, ok := daemons[baseUser]; ok {
			if existing.ip == clientIP {
				// Same machine reconnecting: replace old socket
				existing.conn.Close()
			} else {
				// Different machine with the same username: check if existing is still alive
				_ = existing.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
				_, err := existing.conn.Write([]byte("HEARTBEAT\n"))
				_ = existing.conn.SetWriteDeadline(time.Time{})
				if err != nil {
					existing.conn.Close()
				} else {
					// Keep first user intact; assign discriminator to second user
					for i := 2; ; i++ {
						candidate := fmt.Sprintf("%s#%d", baseUser, i)
						if ex, exists := daemons[candidate]; !exists || ex.ip == clientIP {
							if exists {
								ex.conn.Close()
							}
							assignedUser = candidate
							break
						}
					}
				}
			}
		}
		daemons[assignedUser] = daemonEntry{conn: conn, ip: clientIP}
		mu.Unlock()

		log.Printf("Registered user: %s (%s)", assignedUser, clientIP)

		for {
			time.Sleep(20 * time.Second)
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_, err := conn.Write([]byte("HEARTBEAT\n"))
			_ = conn.SetWriteDeadline(time.Time{})
			if err != nil {
				mu.Lock()
				if cur, ok := daemons[assignedUser]; ok && cur.conn == conn {
					delete(daemons, assignedUser)
				}
				mu.Unlock()
				conn.Close()
				log.Printf("Disconnected user: %s", assignedUser)
				return
			}
		}

	case "LIST":
		mu.Lock()
		var users []string
		for u := range daemons {
			users = append(users, u)
		}
		mu.Unlock()
		sort.Strings(users)
		fmt.Fprintln(conn, strings.Join(users, ","))
		conn.Close()

	case "REQUEST":
		if len(parts) < 3 {
			conn.Close()
			return
		}
		target := strings.ToLower(parts[1])
		sender := strings.ToLower(parts[2])

		mu.Lock()
		entry, online := daemons[target]
		if !online {
			mu.Unlock()
			conn.Write([]byte{2})
			conn.Close()
			return
		}

		b := make([]byte, 8)
		_, _ = rand.Read(b)
		sessionID := hex.EncodeToString(b)
		sessions[sessionID] = pendingSession{
			aliceConn:   conn,
			aliceReader: reader,
			createdAt:   time.Now(),
		}
		mu.Unlock()

		_ = entry.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_, err := fmt.Fprintf(entry.conn, "PING %s %s\n", sessionID, sender)
		_ = entry.conn.SetWriteDeadline(time.Time{})
		if err != nil {
			mu.Lock()
			delete(sessions, sessionID)
			if cur, ok := daemons[target]; ok && cur.conn == entry.conn {
				delete(daemons, target)
			}
			mu.Unlock()
			entry.conn.Close()
			conn.Write([]byte{2})
			conn.Close()
		}

	case "DECISION":
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
			sess.aliceConn.Write([]byte{0})
			sess.aliceConn.Close()
			return
		}
		mu.Unlock()
		sess.aliceConn.Write([]byte{1})

	case "BRIDGE":
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

		sess.aliceConn.Write([]byte{1})

		go func() {
			_, _ = io.Copy(sess.aliceConn, reader)
			sess.aliceConn.Close()
			conn.Close()
		}()
		_, _ = io.Copy(conn, sess.aliceReader)
		conn.Close()
		sess.aliceConn.Close()
	}
}