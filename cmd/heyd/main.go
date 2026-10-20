package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"heychat/internal/discovery"
	"heychat/internal/history"
)

func main() {
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("USERNAME")
	}
	if user == "" {
		user = "peer"
	}

	// 1. Background 10-Minute Garbage Collector (Wipes expired JSON & WAV files)
	go func() {
		for {
			history.GarbageCollectAll()
			time.Sleep(1 * time.Minute)
		}
	}()

	// 2. Global Relay Listener (Auto-reconnects if the server drops)
	go runRelayListener(user)

	// 3. Local LAN mDNS Broadcast & Listener
	
	runLocalListener()
}

func runRelayListener(user string) {
	for {
		conn, err := discovery.DialRelay()
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}

		fmt.Fprintf(conn, "LISTEN %s\n", user)
		scanner := bufio.NewScanner(conn)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			// Expected format from relay: INCOMING <sender> <sessionID>
			if strings.HasPrefix(line, "INCOMING ") {
				parts := strings.SplitN(line, " ", 3)
				if len(parts) == 3 {
					sender, sessionID := parts[1], parts[2]
					if promptNativePopup(sender) {
						conn.Write([]byte{1})
						spawnTerminal("relay:" + sessionID)
					} else {
						conn.Write([]byte{0})
					}
				}
			}
		}
		if err := scanner.Err(); err != nil {
			log.Printf("Relay listener error: %v", err)
		}
		conn.Close()
		time.Sleep(3 * time.Second)
	}
}

func runLocalListener() {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", discovery.Port))
	if err != nil {
		log.Fatalf("Failed to bind daemon port %d: %v", discovery.Port, err)
	}
	defer ln.Close()

	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		go handleLocalRequest(conn)
	}
}

func handleLocalRequest(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return
	}
	line = strings.TrimSpace(line)

	if strings.HasPrefix(line, "CHAT_PORT:") {
		port := strings.TrimPrefix(line, "CHAT_PORT:")
		hostIP, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
		targetAddr := net.JoinHostPort(hostIP, port)

		if promptNativePopup(hostIP) {
			conn.Write([]byte("accepted\n"))
			spawnTerminal(targetAddr)
		} else {
			conn.Write([]byte("declined\n"))
		}
	}
}

// promptNativePopup triggers an OS-native Accept/Decline dialog
func promptNativePopup(sender string) bool {
	msg := fmt.Sprintf("Incoming encrypted chat from %s. Accept?", sender)

	switch runtime.GOOS {
	case "linux":
		// 1. Try modern dunstify/notify-send action buttons
		if path, err := exec.LookPath("dunstify"); err == nil {
			out, err := exec.Command(path, "-A", "yes,Accept", "-A", "no,Decline", "HeyChat", msg).Output()
			if err == nil {
				return strings.TrimSpace(string(out)) == "yes"
			}
		}
		// 2. Fallback to Zenity or KDialog modal
		if path, err := exec.LookPath("zenity"); err == nil {
			return exec.Command(path, "--question", "--title=HeyChat", "--text="+msg).Run() == nil
		}
		if path, err := exec.LookPath("kdialog"); err == nil {
			return exec.Command(path, "--yesno", msg, "--title", "HeyChat").Run() == nil
		}
		// 3. Fallback: notify and auto-spawn terminal so PIN acts as the gatekeeper
		_ = exec.Command("notify-send", "HeyChat", "Incoming chat from "+sender).Run()
		return true

	case "darwin":
		script := fmt.Sprintf(`display dialog %q with title "HeyChat" buttons {"Decline", "Accept"} default button "Accept"`, msg)
		err := exec.Command("osascript", "-e", script).Run()
		return err == nil

	case "windows":
		ps := fmt.Sprintf(`Add-Type -AssemblyName PresentationFramework; [System.Windows.MessageBox]::Show('%s', 'HeyChat', 'YesNo', 'Question')`, msg)
		out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps).Output()
		return err == nil && strings.TrimSpace(string(out)) == "Yes"
	}
	return false
}

// spawnTerminal launches a new OS terminal window running `hey join <targetAddr>`
func spawnTerminal(targetAddr string) {
	switch runtime.GOOS {
	case "linux":
		terms := [][]string{
			{"kitty", "-e", "hey", "join", targetAddr},
			{"alacritty", "-e", "hey", "join", targetAddr},
			{"foot", "hey", "join", targetAddr},
			{"wezterm", "start", "--", "hey", "join", targetAddr},
			{"gnome-terminal", "--", "hey", "join", targetAddr},
			{"konsole", "-e", "hey", "join", targetAddr},
			{"xfce4-terminal", "-x", "hey", "join", targetAddr},
			{"xterm", "-e", "hey", "join", targetAddr},
		}
		for _, args := range terms {
			if path, err := exec.LookPath(args[0]); err == nil {
				_ = exec.Command(path, args[1:]...).Start()
				return
			}
		}

	case "darwin":
		script := fmt.Sprintf(`tell application "Terminal" to do script "~/.local/bin/hey join %s"`, targetAddr)
		_ = exec.Command("osascript", "-e", script).Start()

	case "windows":
		_ = exec.Command("cmd", "/c", "start", "powershell", "-NoExit", "-Command", "hey.exe join "+targetAddr).Start()
	}
}