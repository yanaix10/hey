//go:build linux
package notify

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/godbus/dbus/v5"
)

// detectTerminal finds an available terminal emulator on the system
func detectTerminal() (string, error) {
	if term := os.Getenv("TERMINAL"); term != "" {
		if _, err := exec.LookPath(term); err == nil {
			return term, nil
		}
	}

	candidates := []string{"alacritty", "kitty", "foot", "ghostty", "wezterm", "gnome-terminal", "konsole", "xfce4-terminal", "xterm"}
	for _, c := range candidates {
		if _, err := exec.LookPath(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("no supported terminal emulator found in $PATH")
}

// Prompt shows an interactive notification and blocks until accepted or closed
func Prompt(senderIP string) bool {
	conn, err := dbus.SessionBus()
	if err != nil {
		fmt.Printf("Failed to connect to D-Bus: %v\n", err)
		return false
	}
	defer conn.Close()

	conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0,
		"type='signal',interface='org.freedesktop.Notifications',member='ActionInvoked'")
	conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0,
		"type='signal',interface='org.freedesktop.Notifications',member='NotificationClosed'")

	c := make(chan *dbus.Signal, 10)
	conn.Signal(c)

	obj := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
	actions := []string{"accept", "Accept Chat"}
	hints := map[string]dbus.Variant{}

	var notifID uint32
	err = obj.Call("org.freedesktop.Notifications.Notify", 0,
		"heyd", uint32(0), "dialog-information",
		"Incoming Hey!", fmt.Sprintf("User at %s wants to chat.", senderIP),
		actions, hints, int32(15000)).Store(&notifID)

	if err != nil {
		fmt.Printf("Failed to send notification: %v\n", err)
		return false
	}

	timeout := time.After(15 * time.Second)
	for {
		select {
		case signal := <-c:
			if len(signal.Body) == 0 {
				continue
			}

			returnedID, ok := signal.Body[0].(uint32)
			if !ok || returnedID != notifID {
				continue
			}

			if signal.Name == "org.freedesktop.Notifications.ActionInvoked" && len(signal.Body) > 1 {
				actionKey, ok := signal.Body[1].(string)
				if ok {
					return actionKey == "accept"
				}
			}

			if signal.Name == "org.freedesktop.Notifications.NotificationClosed" {
				return false
			}

		case <-timeout:
			return false
		}
	}
}

// SpawnTerminal launches the terminal and connects to the sender's chat listener
func SpawnTerminal(chatTarget string) {
	term, err := detectTerminal()
	if err != nil {
		fmt.Printf("Terminal spawn failed: %v\n", err)
		return
	}

	// Resolve absolute path to the hey binary
	cwd, _ := os.Getwd()
	binPath := filepath.Join(cwd, "hey")

	// Fallback to searching PATH if not in current directory
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		if path, err := exec.LookPath("hey"); err == nil {
			binPath = path
		}
	}

	// Build command based on emulator type
	var cmd *exec.Cmd
	shellScript := fmt.Sprintf("'%s' join '%s'; echo ''; read -p 'Press enter to exit...' _", binPath, chatTarget)

	switch term {
	case "gnome-terminal":
		cmd = exec.Command(term, "--", "bash", "-c", shellScript)
	default: // alacritty, kitty, foot, ghostty, wezterm, xterm all support -e
		cmd = exec.Command(term, "-e", "bash", "-c", shellScript)
	}

	if err := cmd.Start(); err != nil {
		fmt.Printf("Failed to launch %s: %v\n", term, err)
	} else {
		fmt.Printf("Spawned %s (PID: %d)\n", term, cmd.Process.Pid)
	}
}