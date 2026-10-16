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

	"heychat/internal/discovery"
	"heychat/internal/notify"
)

func main() {
	username := os.Getenv("USER")
	if username == "" {
		username = os.Getenv("USERNAME") // Windows fallback
	}
	if username == "" {
		username = "anonymous"
	}

	server, err := discovery.Register(username)
	if err != nil {
		log.Fatalf("Failed to start mDNS: %v", err)
	}
	defer server.Shutdown()

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", discovery.Port))
	if err != nil {
		log.Fatalf("TCP listener failed: %v", err)
	}
	defer listener.Close()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		server.Shutdown()
		os.Exit(0)
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go handlePing(conn)
	}
}

func handlePing(conn net.Conn) {
	defer conn.Close()

	peerAddr := conn.RemoteAddr().String()
	peerIP := strings.Split(peerAddr, ":")[0]

	// Read session request format: "CHAT_PORT:4243\n"
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

	chatTarget := fmt.Sprintf("%s:%s", peerIP, chatPort)
	fmt.Printf("Chat requested from %s. Asking user...\n", chatTarget)

	if notify.Prompt(peerIP) {
		fmt.Println("Accepted. Spawning terminal...")
		conn.Write([]byte("accepted\n"))
		notify.SpawnTerminal(chatTarget)
	} else {
		fmt.Println("Declined/Ignored.")
		conn.Write([]byte("rejected\n"))
	}
}