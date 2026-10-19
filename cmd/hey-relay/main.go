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
	daemons  = make(map[string]net.Conn)
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

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "4244"
	}

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
		username := parts[1]
		mu.Lock()
		if old, ok := daemons[username]; ok {
			old.Close()
		}
		daemons[username] = conn
		mu.Unlock()
		log.Printf("Registered user: %s", username)

		for {
			time.Sleep(20 * time.Second)
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

	case "REQUEST":
		if len(parts) < 3 {
			conn.Close()
			return
		}
		target, sender := parts[1], parts[2]

		mu.Lock()
		daemonConn, online := daemons[target]
		if !online {
			mu.Unlock()
			conn.Write([]byte{2})
			conn.Close()
			return
		}

		b := make([]byte, 8)
		_, _ = rand.Read(b)
		sessionID := hex.EncodeToString(b)
		sessions[sessionID] = pendingSession{aliceConn: conn, aliceReader: reader}
		mu.Unlock()

		if _, err := fmt.Fprintf(daemonConn, "PING %s %s\n", sessionID, sender); err != nil {
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