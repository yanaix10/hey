//go:build darwin

package notify

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func Prompt(senderIP string) bool {
	script := fmt.Sprintf(`display dialog "User at %s wants to chat." with title "Incoming Hey!" buttons {"Decline", "Accept"} default button "Accept"`, senderIP)
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "button returned:Accept")
}

func SpawnTerminal(chatTarget string) {
	exePath, _ := os.Executable()
	heyBin := filepath.Join(filepath.Dir(exePath), "hey")
	if _, err := os.Stat(heyBin); os.IsNotExist(err) {
		if path, err := exec.LookPath("hey"); err == nil {
			heyBin = path
		}
	}

	script := fmt.Sprintf(`tell application "Terminal"
		activate
		do script "'%s' join '%s'"
	end tell`, heyBin, chatTarget)
	_ = exec.Command("osascript", "-e", script).Start()
}
