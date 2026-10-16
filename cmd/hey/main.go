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

	"heychat/internal/auth"
	"heychat/internal/discovery"
	"heychat/internal/ui"
)

const SessionPort = 4243

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage:\n  hey <username>       (Initiate chat)\n  hey join <ip:port>   (Join chat)")
		os.Exit(1)
	}

	if os.Args[1] == "join" {
		if len(os.Args) < 3 {
			log.Fatal("Usage: hey join <ip:port>")
		}
		runJoin(os.Args[2])
		return
	}

	runSend(os.Args[1])
}

// runSend handles Alice's side: Listen on 4243, notify Bob, perform handshake
func runSend(target string) {
	target = strings.TrimPrefix(target, "hey@")
	target = strings.TrimPrefix(target, "@")

	// 1. Generate 6-digit confirmation PIN
	pin, err := auth.GeneratePIN()
	if err != nil {
		log.Fatalf("Error generating PIN: %v", err)
	}

	// 2. Open chat listener on 4243
	chatListener, err := net.Listen("tcp", fmt.Sprintf(":%d", SessionPort))
	if err != nil {
		log.Fatalf("Failed to bind chat session socket: %v", err)
	}
	defer chatListener.Close()

	// 3. Resolve target via mDNS
	fmt.Printf("Resolving %s on local network...\n", target)
	ip, err := discovery.Lookup(target)
	if err != nil {
		log.Fatalf("Lookup failed: %v", err)
	}

	// 4. Send ping to Bob's daemon
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", ip, discovery.Port))
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

	fmt.Println("Target accepted request!")
	fmt.Printf("\n=========================================\n")
	fmt.Printf("   YOUR CONFIRMATION PIN IS:  %s        \n", pin)
	fmt.Printf("=========================================\n")
	fmt.Println("Waiting for peer to enter code...")

	// 5. Accept Bob's incoming chat connection
	peerConn, err := chatListener.Accept()
	if err != nil {
		log.Fatalf("Failed to accept peer connection: %v", err)
	}
	defer peerConn.Close()

	// 6. Cryptographic Challenge Handshake
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	peerConn.Write(salt) // Send challenge salt

	// Read peer's response
	responseHash := make([]byte, 32)
	_, err = io.ReadFull(peerConn, responseHash)
	if err != nil {
		log.Fatalf("Peer disconnected during handshake.")
	}

	// Verify proof
	expectedHash := sha256.Sum256(append([]byte(pin), salt...))
	if !bytes.Equal(responseHash, expectedHash[:]) {
		peerConn.Write([]byte{0}) // Failure signal
		log.Fatal("\n[!] Handshake failed: Incorrect PIN entered by peer.")
	}

	peerConn.Write([]byte{1}) // Success signal
	sessionKey := auth.DeriveKey(pin, salt)

	err = ui.StartChat(peerConn, sessionKey)
	if err != nil {
		log.Printf("Chat ended: %v\n", err)
	}

	// Test encrypted ping
	ciphertext, _ := auth.Encrypt(sessionKey, []byte("Hello, secure peer!"))
	peerConn.Write(ciphertext)

	fmt.Println("\n[✓] Mutual Authentication verified! Secure session key generated.")
	fmt.Println("(Chat UI will hook in here in Phase 4)")
}

// runJoin handles Bob's side: Connect to Alice, prompt for PIN, prove identity
func runJoin(targetAddr string) {
	fmt.Printf("Connecting to %s...\n", targetAddr)
	conn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// 1. Read 16-byte challenge salt
	salt := make([]byte, 16)
	_, err = io.ReadFull(conn, salt)
	if err != nil {
		log.Fatalf("Handshake failed to receive challenge.")
	}

	// 2. Prompt user for PIN
	fmt.Print("\nEnter 6-digit confirmation code: ")
	var enteredPIN string
	fmt.Scanln(&enteredPIN)
	enteredPIN = strings.TrimSpace(enteredPIN)

	// 3. Compute response hash
	responseHash := sha256.Sum256(append([]byte(enteredPIN), salt...))
	conn.Write(responseHash[:])

	// 4. Check verdict
	verdict := make([]byte, 1)
	_, _ = io.ReadFull(conn, verdict)
	if verdict[0] != 1 {
		log.Fatal("\n[!] Connection rejected: Invalid PIN.")
	}

	sessionKey := auth.DeriveKey(enteredPIN, salt)

	err = ui.StartChat(conn, sessionKey)
	if err != nil {
		log.Printf("Chat ended: %v\n", err)
	}

	// Receive encrypted greeting
	buf := make([]byte, 128)
	n, _ := conn.Read(buf)
	decrypted, err := auth.Decrypt(sessionKey, buf[:n])
	if err != nil {
		log.Fatalf("Failed to decrypt incoming packet: %v", err)
	}

	fmt.Printf("\n[✓] Handshake successful!\nDecrypted from peer: %s\n", string(decrypted))
}