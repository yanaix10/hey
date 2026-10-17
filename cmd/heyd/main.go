package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"heychat/internal/discovery"
	"heychat/internal/notify"
)

func main() {
	username := os.Getenv("USER")
	if username == "" {
		username = os.Getenv("USERNAME")
	}
	if username == "" {
		username = "anonymous"
	}

	server, err := discovery.Register(username)
	if err != nil {
		log.Printf("Warning: mDNS failed: %v", err)
	} else {
		defer server.Shutdown()
	}

	// Connect to global relay in the background
	go maintainRelayConn(username)

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", discovery.Port))
	if err != nil {
		log.Fatalf("TCP listener failed: %v", err)
	}
	defer listener.Close()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		if server != nil {
			server.Shutdown()
		}
		os.Exit(0)
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go handleLocalPing(conn)
	}
}

func maintainRelayConn(username string) {
	relayAddr := discovery.GetRelayAddr()
	for {
		conn, err := net.DialTimeout("tcp", relayAddr, 5*time.Second)
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}

		fmt.Fprintf(conn, "REGISTER %s\n", username)
		reader := bufio.NewReader(conn)

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				conn.Close()
				break
			}
			parts := strings.Fields(strings.TrimSpace(line))
			if len(parts) == 3 && parts[0] == "PING" {
				go handleRelayPing(relayAddr, parts[1], parts[2])
			}
		}
		time.Sleep(3 * time.Second)
	}
}

func handleRelayPing(relayAddr, sessionID, sender string) {
	accepted := notify.Prompt(sender + " (via Relay)")
	decisionConn, err := net.DialTimeout("tcp", relayAddr, 5*time.Second)
	if err != nil {
		return
	}
	defer decisionConn.Close()

	if accepted {
		fmt.Fprintf(decisionConn, "DECISION %s accepted\n", sessionID)
		notify.SpawnTerminal("relay:" + sessionID)
	} else {
		fmt.Fprintf(decisionConn, "DECISION %s rejected\n", sessionID)
	}
}

func handleLocalPing(conn net.Conn) {
	defer conn.Close()

	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return
	}
	// Normalize IPv4-mapped IPv6 address (e.g. ::ffff:192.168.1.5 -> 192.168.1.5)
	if ip := net.ParseIP(host); ip != nil && ip.To4() != nil {
		host = ip.To4().String()
	}

	reader := bufio.NewReader(conn)
	msg, err := reader.ReadString('\n')
	if err != nil {
		return
	}

	chatPort := "4243"
	parts := strings.Split(strings.TrimSpace(msg), ":")
	if len(parts) == 2 && parts[0] == "CHAT_PORT" {
		chatPort = parts[1]
	}

	chatTarget := net.JoinHostPort(host, chatPort)
	if notify.Prompt(host) {
		conn.Write([]byte("accepted\n"))
		notify.SpawnTerminal(chatTarget)
	} else {
		conn.Write([]byte("rejected\n"))
	}
}
