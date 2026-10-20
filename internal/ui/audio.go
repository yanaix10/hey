package ui

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// StartRecording detects the available OS audio backend and begins recording a 16kHz mono WAV.
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
		// Fallback chain: PipeWire -> PulseAudio -> ALSA -> FFmpeg
		if path, err := exec.LookPath("pw-record"); err == nil {
			cmd = exec.Command(path, "--format=s16", "--rate=16000", "--channels=1", filePath)
		} else if path, err := exec.LookPath("parecord"); err == nil {
			cmd = exec.Command(path, "--file-format=wav", "--format=s16le", "--rate=16000", "--channels=1", filePath)
		} else if path, err := exec.LookPath("arecord"); err == nil {
			cmd = exec.Command(path, "-q", "-f", "S16_LE", "-r", "16000", "-c", "1", "-t", "wav", filePath)
		} else if path, err := exec.LookPath("ffmpeg"); err == nil {
			cmd = exec.Command(path, "-loglevel", "quiet", "-y", "-f", "pulse", "-i", "default", "-ac", "1", "-ar", "16000", filePath)
		} else {
			_ = os.Remove(filePath)
			return nil, "", fmt.Errorf("no recorder found (install pipewire, pulseaudio, or alsa-utils)")
		}

	case "darwin":
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
		_ = os.Remove(filePath)
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
			_ = cmd.Process.Signal(os.Interrupt)
			_ = cmd.Wait()
		}
	}

	defer os.Remove(filePath)

	data, err := os.ReadFile(filePath)
	if err != nil || len(data) <= 44 { // 44 bytes is an empty WAV header
		return "", fmt.Errorf("recorded audio is empty")
	}

	return base64.StdEncoding.EncodeToString(data), nil
}

// CancelRecording kills the recording process and deletes the temp file.
func CancelRecording(cmd *exec.Cmd, filePath string) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	_ = os.Remove(filePath)
	_ = os.Remove(filePath + ".stop")
}

// GetVoiceNotePath returns the local file path for a numbered voice note.
func GetVoiceNotePath(seqID string) string {
	return filepath.Join(voiceHistoryDir(), "voice_"+seqID+".wav")
}

// SaveVoiceNote saves base64 audio to the history folder so it can be played or replayed.
func SaveVoiceNote(seqID, base64Audio string) (string, error) {
	rawAudio, err := base64.StdEncoding.DecodeString(base64Audio)
	if err != nil {
		return "", err
	}
	filePath := GetVoiceNotePath(seqID)
	if err := os.WriteFile(filePath, rawAudio, 0600); err != nil {
		return "", err
	}
	return filePath, nil
}

// PlayVoiceNoteByID plays a saved voice note by its sequence number (1, 2, 3...).
func PlayVoiceNoteByID(seqID string) error {
	filePath := GetVoiceNotePath(seqID)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("voice note #%s not found or expired", seqID)
	}
	return PlayAudio(filePath)
}

// DeleteVoiceNote removes a saved voice note file from disk.
func DeleteVoiceNote(seqID string) {
	if seqID == "" {
		return
	}
	_ = os.Remove(GetVoiceNotePath(seqID))
}

// PlayAudio plays a .wav file in the background using the available OS audio player.
func PlayAudio(filePath string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		// Fallback chain: PipeWire -> PulseAudio -> ALSA -> FFplay
		if path, err := exec.LookPath("pw-play"); err == nil {
			cmd = exec.Command(path, filePath)
		} else if path, err := exec.LookPath("paplay"); err == nil {
			cmd = exec.Command(path, filePath)
		} else if path, err := exec.LookPath("aplay"); err == nil {
			cmd = exec.Command(path, "-q", filePath)
		} else if path, err := exec.LookPath("ffplay"); err == nil {
			cmd = exec.Command(path, "-nodisp", "-autoexit", "-loglevel", "quiet", filePath)
		} else {
			return fmt.Errorf("no audio player found (install pipewire, pulseaudio, or alsa-utils)")
		}
	case "darwin":
		cmd = exec.Command("afplay", filePath)
	case "windows":
		psPlay := fmt.Sprintf(`(New-Object System.Media.SoundPlayer '%s').PlaySync()`, filePath)
		cmd = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psPlay)
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
	return cmd.Start()
}