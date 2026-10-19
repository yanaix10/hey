//go:build windows

package notify

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	user32          = syscall.NewLazyDLL("user32.dll")
	procMessageBoxW = user32.NewProc("MessageBoxW")
)

const (
	mbYesNo         = 0x00000004
	mbIconQuestion  = 0x00000020
	mbSystemModal   = 0x00001000
	mbSetForeground = 0x00010000
	idYes           = 6
)

func Prompt(senderIP string) bool {
	title, _ := syscall.UTF16PtrFromString("Incoming Hey!")
	text, _ := syscall.UTF16PtrFromString(fmt.Sprintf("User at %s wants to chat.\nAccept connection?", senderIP))
	flags := uintptr(mbYesNo | mbIconQuestion | mbSystemModal | mbSetForeground)

	ret, _, _ := procMessageBoxW.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), flags)
	return ret == idYes
}

func SpawnTerminal(chatTarget string) {
	exePath, _ := os.Executable()
	binDir := filepath.Dir(exePath)
	heyBin := filepath.Join(binDir, "hey.exe")

	if _, err := os.Stat(heyBin); os.IsNotExist(err) {
		if path, err := exec.LookPath("hey.exe"); err == nil {
			heyBin = path
		}
	}

	// Added explicit error catching and -NoExit so the window stays open to show crashes
	psScript := fmt.Sprintf("try { & '%s' join '%s' } catch { $_; Read-Host 'Error occurred. Press Enter to exit' }", heyBin, chatTarget)
	
	cmd := exec.Command("cmd.exe", "/c", "start", "Hey Chat", "powershell.exe", "-NoLogo", "-NoExit", "-Command", psScript)
	_ = cmd.Start()
}