package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"heychat/internal/auth"
	"heychat/internal/discovery"
	"heychat/internal/ui"
)

const SessionPort = 4243

// --- PEER MANAGEMENT ---

func peersFilePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	heyDir := filepath.Join(dir, "hey")
	_ = os.MkdirAll(heyDir, 0700)
	return filepath.Join(heyDir, "peers.json")
}

func loadPeers() map[string]string {
	peers := make(map[string]string)
	data, err := os.ReadFile(peersFilePath())
	if err == nil {
		_ = json.Unmarshal(data, &peers)
	}
	return peers
}

func savePeerMap(peers map[string]string) {
	data, _ := json.MarshalIndent(peers, "", "  ")
	_ = os.WriteFile(peersFilePath(), data, 0600)
}

func savePeer(username, secret string) {
	peers := loadPeers()
	peers[username] = secret
	savePeerMap(peers)
}

func listPeers() {
	peers := loadPeers()
	if len(peers) == 0 {
		fmt.Println("No trusted peers found.")
		return
	}
	fmt.Println("Trusted Peers:")
	for username := range peers {
		fmt.Println("-", username)
	}
}

func unpair(username string) {
	peers := loadPeers()
	if _, exists := peers[username]; exists {
		delete(peers, username)
		savePeerMap(peers)
		fmt.Printf("Unpaired from '%s'. They will need an OTP to connect again.\n", username)
	} else {
		fmt.Printf("User '%s' not found in trusted peers.\n", username)
	}
}

func clearPeers() {
	_ = os.Remove(peersFilePath())
	fmt.Println("All trusted peers cleared.")
}

// --------------------------------

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage:\n  hey <username>\n  hey join <target>\n  hey peers\n  hey unpair <user>\n  hey clear")
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "join":
		if len(os.Args) < 3 {
			log.Fatal("Usage: hey join <target>")
		}
		runJoin(os.Args[2])
	case "peers":
		listPeers()
	case "unpair":
		if len(os.Args) < 3 {
			fmt.Println("Usage: hey unpair <username>")
			return
		}
		unpair(os.Args[2])
	case "clear":
		clearPeers()
	default:
		runSend(command)
	}
}

func runSend(target string) {
	target = strings.TrimPrefix(target, "hey@")
	target = strings.TrimPrefix(target, "@")

	// Check if peer is already trusted in peers.json
	peers := loadPeers()
	savedPin, isTrusted := peers[target]

	var pin string
	var err error

	if isTrusted {
		pin = savedPin
		fmt.Printf("Connecting to trusted peer %s (skipping OTP)...\n", target)
	} else {
		pin, err = auth.GeneratePIN()
		if err != nil {
			log.Fatalf("Error generating PIN: %v", err)
		}
	}

	fmt.Printf("Looking for %s on local Wi-Fi...\n", target)
	ip, err := discovery.Lookup(target)
	if err == nil {
		runLocalSend(target, ip, pin, isTrusted)
		return
	}

	fmt.Printf("Not on local Wi-Fi. Contacting global relay for %s...\n", target)
	runRelaySend(target, pin, isTrusted)
}

func runLocalSend(target, ip, pin string, isTrusted bool) {
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

	if !isTrusted {
		printPIN(pin)
	}

	peerConn, err := chatListener.Accept()
	if err != nil {
		log.Fatalf("Failed to accept peer connection: %v", err)
	}
	defer peerConn.Close()

	completeHostHandshake(peerConn, pin)
}

func runRelaySend(target, pin string, isTrusted bool) {
	sender := os.Getenv("USER")
	if sender == "" {
		sender = os.Getenv("USERNAME")
	}
	if sender == "" {
		sender = "peer"
	}

	conn, err := discovery.DialRelay()
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

	if !isTrusted {
		printPIN(pin)
	}

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
	_, _ = peerConn.Write(salt)

	responseHash := make([]byte, 32)
	if _, err := io.ReadFull(peerConn, responseHash); err != nil {
		log.Fatalf("Peer disconnected during handshake.")
	}

	expectedHash := sha256.Sum256(append([]byte(pin), salt...))
	if !bytes.Equal(responseHash, expectedHash[:]) {
		_, _ = peerConn.Write([]byte{0})
		log.Fatal("\n[!] Handshake failed: PIN mismatch.")
	}

	_, _ = peerConn.Write([]byte{1})
	sessionKey := auth.DeriveKey(pin, salt)

	localUser := os.Getenv("USER")
	if localUser == "" {
		localUser = os.Getenv("USERNAME")
	}
	if localUser == "" {
		localUser = "host"
	}

	_, _ = peerConn.Write([]byte(localUser + "\n"))
	reader := bufio.NewReader(peerConn)
	peerUser, _ := reader.ReadString('\n')
	peerUser = strings.TrimSpace(peerUser)
	if peerUser == "" {
		peerUser = "Peer"
	}

	savePeer(peerUser, pin)

	if err := ui.StartChat(peerConn, sessionKey, peerUser); err != nil {
		log.Printf("Chat ended: %v\n", err)
	}
}

func runJoin(targetAddr string) {
	var conn net.Conn
	var err error

	if strings.HasPrefix(targetAddr, "relay:") {
		sessionID := strings.TrimPrefix(targetAddr, "relay:")
		conn, err = discovery.DialRelay()
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

	// Read peer's username early to check if they are trusted
	localUser := os.Getenv("USER")
	if localUser == "" {
		localUser = os.Getenv("USERNAME")
	}

	// We send our username first so the joining side knows who is connecting
	// (or we can prompt if not trusted)
	var enteredPIN string
	peers := loadPeers()

	// Prompt for PIN if not in trusted peers
	fmt.Print("\nEnter 6-digit confirmation code (Press Enter if trusted peer): ")
	stdinReader := bufio.NewReader(os.Stdin)
	enteredPIN, _ = stdinReader.ReadString('\n')
	enteredPIN = strings.TrimSpace(enteredPIN)

	// If empty input, check if we have any matching peer or if we can infer
	if enteredPIN == "" {
		// If we only have one trusted peer or want to attempt auto-match
		for _, p := range peers {
			enteredPIN = p
			break
		}
	}

	if enteredPIN == "" {
		log.Fatal("PIN required for unknown peer.")
	}

	responseHash := sha256.Sum256(append([]byte(enteredPIN), salt...))
	_, _ = conn.Write(responseHash[:])

	verdict := make([]byte, 1)
	_, _ = io.ReadFull(conn, verdict)
	if verdict[0] != 1 {
		log.Fatal("\n[!] Connection rejected: Invalid PIN.")
	}

	sessionKey := auth.DeriveKey(enteredPIN, salt)

	reader := bufio.NewReader(conn)
	peerUser, _ := reader.ReadString('\n')
	peerUser = strings.TrimSpace(peerUser)
	if peerUser == "" {
		peerUser = "Peer"
	}

	_, _ = conn.Write([]byte(localUser + "\n"))

	savePeer(peerUser, enteredPIN)

	if err := ui.StartChat(conn, sessionKey, peerUser); err != nil {
		log.Printf("Chat ended: %v\n", err)
	}
}