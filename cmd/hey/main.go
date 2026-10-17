package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"bufio"

	"heychat/internal/auth"
	"heychat/internal/discovery"
	"heychat/internal/ui"
)

const SessionPort = 4243

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage:\n  hey <username>\n  hey join <target>")
		os.Exit(1)
	}

	if os.Args[1] == "join" {
		if len(os.Args) < 3 {
			log.Fatal("Usage: hey join <target>")
		}
		runJoin(os.Args[2])
		return
	}

	runSend(os.Args[1])
}

func runSend(target string) {
	target = strings.TrimPrefix(target, "hey@")
	target = strings.TrimPrefix(target, "@")

	pin, err := auth.GeneratePIN()
	if err != nil {
		log.Fatalf("Error generating PIN: %v", err)
	}

	fmt.Printf("Looking for %s on local Wi-Fi...\n", target)
	ip, err := discovery.Lookup(target)
	if err == nil {
		runLocalSend(target, ip, pin)
		return
	}

	fmt.Printf("Not on local Wi-Fi. Contacting global relay for %s...\n", target)
	runRelaySend(target, pin)
}

func runLocalSend(target, ip, pin string) {
	chatListener, err := net.Listen("tcp", fmt.Sprintf(":%d", SessionPort))
	if err != nil {
		log.Fatalf("Failed to bind chat port: %v", err)
	}
	defer chatListener.Close()

	conn, err := net.Dial("tcp", net.JoinHostPort(ip, fmt.Sprintf("%d", discovery.Port)))
	if err != nil {
		log.Fatalf("Could not reach %s: %v", target, err)
	}
	fmt.Fprintf(conn, "CHAT_PORT:%d\n", SessionPort)

	resp := make([]byte, 32)
	n, _ := conn.Read(resp)
	conn.Close()

	if !strings.Contains(string(resp[:n]), "accepted") {
		fmt.Println("User declined or request timed out.")
		return
	}

	printPIN(pin)
	peerConn, err := chatListener.Accept()
	if err != nil {
		log.Fatalf("Failed to accept peer connection: %v", err)
	}
	defer peerConn.Close()

	completeHostHandshake(peerConn, pin)
}

func runRelaySend(target, pin string) {
	sender := os.Getenv("USER")
	if sender == "" {
		sender = os.Getenv("USERNAME")
	}
	if sender == "" {
		sender = "peer"
	}

	conn, err := net.Dial("tcp", discovery.GetRelayAddr())
	if err != nil {
		log.Fatalf("Could not connect to relay: %v", err)
	}
	defer conn.Close()

	fmt.Fprintf(conn, "REQUEST %s %s\n", target, sender)

	status := make([]byte, 1)
	if _, err := io.ReadFull(conn, status); err != nil || status[0] != 1 {
		if status[0] == 2 {
			fmt.Printf("User '%s' is offline.\n", target)
		} else {
			fmt.Println("User declined or request timed out.")
		}
		return
	}

	printPIN(pin)

	// Wait for peer's spawned terminal to bridge into the relay
	if _, err := io.ReadFull(conn, status); err != nil || status[0] != 1 {
		log.Fatal("Peer terminal failed to connect.")
	}

	completeHostHandshake(conn, pin)
}

func printPIN(pin string) {
	fmt.Println("Target accepted request!")
	fmt.Printf("\n=========================================\n")
	fmt.Printf("   YOUR CONFIRMATION PIN IS:  %s        \n", pin)
	fmt.Printf("=========================================\n")
	fmt.Println("Waiting for peer to enter code...")
}

func completeHostHandshake(peerConn net.Conn, pin string) {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	peerConn.Write(salt)

	responseHash := make([]byte, 32)
	if _, err := io.ReadFull(peerConn, responseHash); err != nil {
		log.Fatalf("Peer disconnected during handshake.")
	}

	expectedHash := sha256.Sum256(append([]byte(pin), salt...))
	if !bytes.Equal(responseHash, expectedHash[:]) {
		peerConn.Write([]byte{0})
		log.Fatal("\n[!] Handshake failed: Incorrect PIN entered by peer.")
	}

	peerConn.Write([]byte{1})
	sessionKey := auth.DeriveKey(pin, salt)

	if err := ui.StartChat(peerConn, sessionKey); err != nil {
		log.Printf("Chat ended: %v\n", err)
	}
}

func runJoin(targetAddr string) {
	var conn net.Conn
	var err error

	if strings.HasPrefix(targetAddr, "relay:") {
		sessionID := strings.TrimPrefix(targetAddr, "relay:")
		conn, err = net.Dial("tcp", discovery.GetRelayAddr())
		if err != nil {
			log.Fatalf("Failed to connect to relay: %v", err)
		}
		fmt.Fprintf(conn, "BRIDGE %s\n", sessionID)
	} else {
		conn, err = net.Dial("tcp", targetAddr)
		if err != nil {
			log.Fatalf("Failed to connect: %v", err)
		}
	}
	defer conn.Close()

	salt := make([]byte, 16)
	if _, err := io.ReadFull(conn, salt); err != nil {
		log.Fatalf("Handshake failed to receive challenge.")
	}

	fmt.Print("\nEnter 6-digit confirmation code: ")
	stdinReader := bufio.NewReader(os.Stdin)
	enteredPIN, _ := stdinReader.ReadString('\n')
	enteredPIN = strings.TrimSpace(enteredPIN)

	responseHash := sha256.Sum256(append([]byte(enteredPIN), salt...))
	conn.Write(responseHash[:])

	verdict := make([]byte, 1)
	_, _ = io.ReadFull(conn, verdict)
	if verdict[0] != 1 {
		log.Fatal("\n[!] Connection rejected: Invalid PIN.")
	}

	sessionKey := auth.DeriveKey(enteredPIN, salt)
	if err := ui.StartChat(conn, sessionKey); err != nil {
		log.Printf("Chat ended: %v\n", err)
	}
}
