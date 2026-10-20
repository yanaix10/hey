package ui

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

const MaxRecordSeconds = 30

func voiceHistoryDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	path := filepath.Join(dir, "hey", "history")
	_ = os.MkdirAll(path, 0700)
	return path
}

// CheckFirstTimeMicPermission returns true on the very first recording attempt
// and creates a marker file so the prompt only happens once.
func CheckFirstTimeMicPermission() bool {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	heyDir := filepath.Join(dir, "hey")
	_ = os.MkdirAll(heyDir, 0700)
	marker := filepath.Join(heyDir, ".mic_permission_granted")

	if _, err := os.Stat(marker); os.IsNotExist(err) {
		_ = os.WriteFile(marker, []byte("1"), 0600)
		return true
	}
	return false
}

// StartRecording starts the OS native mic tool (capped at 30s) and returns the command and temp file path.
func StartRecording() (*exec.Cmd, string, error) {
	file, err := os.CreateTemp("", "hey_voice_*.wav")
	if err != nil {
		return nil, "", err
	}
	filePath := file.Name()
	_ = file.Close()

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		// 16kHz, 16-bit mono WAV, max 30 seconds
		cmd = exec.Command("arecord", "-q", "-f", "S16_LE", "-r", "16000", "-c", "1", "-d", "30", "-t", "wav", filePath)
	case "darwin":
		// macOS native AudioToolbox recorder, 16kHz mono WAV, max 30 seconds
		cmd = exec.Command("afrecord", "-d", "30", "-f", "WAVE", "-d", "LEI16@16000", "-c", "1", filePath)
	case "windows":
		stopFile := filePath + ".stop"
		psScript := fmt.Sprintf(`
$code = '[DllImport("winmm.dll")] public static extern int mciSendString(string cmd, string buf, int len, IntPtr cb);'
Add-Type -MemberDefinition $code -Name "Audio" -Namespace "Win32"
[Win32.Audio]::mciSendString("open new type waveaudio alias hw", $null, 0, [IntPtr]::Zero) | Out-Null
[Win32.Audio]::mciSendString("set hw time format ms bitspersample 16 channels 1 samplespersec 16000", $null, 0, [IntPtr]::Zero) | Out-Null
[Win32.Audio]::mciSendString("record hw", $null, 0, [IntPtr]::Zero) | Out-Null
for ($i = 0; $i -lt 300; $i++) {
    if (Test-Path '%s') { break }
    Start-Sleep -Milliseconds 100
}
[Win32.Audio]::mciSendString("stop hw", $null, 0, [IntPtr]::Zero) | Out-Null
[Win32.Audio]::mciSendString('save hw "%s"', $null, 0, [IntPtr]::Zero) | Out-Null
[Win32.Audio]::mciSendString("close hw", $null, 0, [IntPtr]::Zero) | Out-Null
`, stopFile, filePath)
		cmd = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
	default:
		return nil, "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	if err := cmd.Start(); err != nil {
		_ = os.Remove(filePath)
		return nil, "", err
	}
	return cmd, filePath, nil
}

// StopRecording gracefully stops the recorder, encodes the WAV file to base64, and removes the temp file.
func StopRecording(cmd *exec.Cmd, filePath string) (string, error) {
	if cmd != nil && cmd.Process != nil {
		if runtime.GOOS == "windows" {
			stopFile := filePath + ".stop"
			_ = os.WriteFile(stopFile, []byte("1"), 0600)
			_ = cmd.Wait()
			_ = os.Remove(stopFile)
		} else {
			// Send SIGINT so arecord/afrecord finalizes the WAV header before exiting
			_ = cmd.Process.Signal(os.Interrupt)
			_ = cmd.Wait()
		}
	}

	defer os.Remove(filePath)

	data, err := os.ReadFile(filePath)
	if err != nil || len(data) == 0 {
		return "", fmt.Errorf("failed to read recorded audio")
	}

	return base64.StdEncoding.EncodeToString(data), nil
}

// CancelRecording immediately kills the recording process and deletes the temp file.
func CancelRecording(cmd *exec.Cmd, filePath string) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	_ = os.Remove(filePath)
	_ = os.Remove(filePath + ".stop")
}

// SaveAndPlayVoiceNote decodes base64 audio into the history folder (for 10-min auto-cleanup) and plays it.
func SaveAndPlayVoiceNote(msgID, base64Audio string) (string, error) {
	rawAudio, err := base64.StdEncoding.DecodeString(base64Audio)
	if err != nil {
		return "", err
	}

	if msgID == "" {
		msgID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	filePath := filepath.Join(voiceHistoryDir(), "voice_"+msgID+".wav")

	if err := os.WriteFile(filePath, rawAudio, 0600); err != nil {
		return "", err
	}

	PlayAudio(filePath)
	return filePath, nil
}

// DeleteVoiceNote removes a saved voice note file from disk (used when /delete is triggered).
func DeleteVoiceNote(msgID string) {
	if msgID == "" {
		return
	}
	filePath := filepath.Join(voiceHistoryDir(), "voice_"+msgID+".wav")
	_ = os.Remove(filePath)
}

// PlayAudio plays a .wav file in the background using native OS tools.
func PlayAudio(filePath string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("aplay", "-q", filePath)
	case "darwin":
		cmd = exec.Command("afplay", filePath)
	case "windows":
		psPlay := fmt.Sprintf(`(New-Object System.Media.SoundPlayer '%s').PlaySync()`, filePath)
		cmd = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psPlay)
	default:
		return
	}
	_ = cmd.Start()
}