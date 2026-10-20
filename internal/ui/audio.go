package ui

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
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

// StartRecording starts the OS native microphone recorder.
func StartRecording() (*exec.Cmd, string, error) {
	file, err := os.CreateTemp("", "hey_voice_*.wav")
	if err != nil {
		return nil, "", err
	}
	filePath := file.Name()
	_ = file.Close()
	// Remove the 0-byte placeholder so Windows MCI and Linux tools can create it cleanly
	_ = os.Remove(filePath)

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		if path, err := exec.LookPath("parecord"); err == nil {
			cmd = exec.Command(path, "--file-format=wav", "--format=s16le", "--rate=16000", "--channels=1", filePath)
		} else if path, err := exec.LookPath("pw-record"); err == nil {
			cmd = exec.Command(path, "--format=s16", "--rate=16000", "--channels=1", filePath)
		} else if path, err := exec.LookPath("arecord"); err == nil {
			cmd = exec.Command(path, "-q", "-f", "S16_LE", "-r", "16000", "-c", "1", "-t", "wav", filePath)
		} else if path, err := exec.LookPath("ffmpeg"); err == nil {
			cmd = exec.Command(path, "-loglevel", "quiet", "-y", "-f", "pulse", "-i", "default", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", filePath)
		} else {
			return nil, "", fmt.Errorf("no recorder found (install pipewire-pulse or alsa-utils)")
		}

	case "darwin":
		cmd = exec.Command("afrecord", "-d", "30", "-f", "WAVE", "-d", "LEI16@16000", "-c", "1", filePath)

	case "windows":
		stopFile := filePath + ".stop"
		_ = os.Remove(stopFile)
		psScript := fmt.Sprintf(`
$code = '[DllImport("winmm.dll", CharSet = CharSet.Auto)] public static extern int mciSendString(string cmd, System.Text.StringBuilder buf, int len, IntPtr cb);'
Add-Type -MemberDefinition $code -Name "Audio" -Namespace "Win32"
[Win32.Audio]::mciSendString("open new type waveaudio alias hw", $null, 0, [IntPtr]::Zero) | Out-Null
[Win32.Audio]::mciSendString("set hw bitspersample 16 channels 1 alignment 2 samplespersec 16000 bytespersec 32000", $null, 0, [IntPtr]::Zero) | Out-Null
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

// StopRecording stops the recorder, normalizes the WAV header to standard PCM, and returns base64 + duration.
func StopRecording(cmd *exec.Cmd, filePath string) (string, int, error) {
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

	raw, err := os.ReadFile(filePath)
	if err != nil || len(raw) <= 44 {
		return "", 0, fmt.Errorf("recorded audio is empty")
	}

	cleanWav, durationSec, err := normalizeWAV(raw)
	if err != nil {
		return "", 0, err
	}

	return base64.StdEncoding.EncodeToString(cleanWav), durationSec, nil
}

// normalizeWAV converts extensible or unfinalized WAV headers into a standard 44-byte PCM WAV header.
func normalizeWAV(raw []byte) ([]byte, int, error) {
	channels := uint16(1)
	sampleRate := uint32(16000)
	bitsPerSample := uint16(16)
	var pcm []byte

	headerSearchLimit := len(raw)
	if headerSearchLimit > 512 {
		headerSearchLimit = 512
	}

	if bytes.HasPrefix(raw, []byte("RIFF")) {
		if fmtIdx := bytes.Index(raw[:headerSearchLimit], []byte("fmt ")); fmtIdx != -1 && fmtIdx+24 <= len(raw) {
			ch := binary.LittleEndian.Uint16(raw[fmtIdx+10 : fmtIdx+12])
			sr := binary.LittleEndian.Uint32(raw[fmtIdx+12 : fmtIdx+16])
			bps := binary.LittleEndian.Uint16(raw[fmtIdx+22 : fmtIdx+24])
			if ch >= 1 && ch <= 2 {
				channels = ch
			}
			if sr >= 8000 && sr <= 96000 {
				sampleRate = sr
			}
			if bps == 8 || bps == 16 || bps == 24 || bps == 32 {
				bitsPerSample = bps
			}
		}
		if dataIdx := bytes.Index(raw[:headerSearchLimit], []byte("data")); dataIdx != -1 && dataIdx+8 <= len(raw) {
			pcm = raw[dataIdx+8:]
		} else if len(raw) > 44 {
			pcm = raw[44:]
		}
	} else {
		pcm = raw
	}

	if len(pcm) < 500 {
		return nil, 0, fmt.Errorf("recorded audio is empty")
	}

	byteRate := sampleRate * uint32(channels) * uint32(bitsPerSample/8)
	blockAlign := channels * (bitsPerSample / 8)

	out := make([]byte, 44+len(pcm))
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(36+len(pcm)))
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16)
	binary.LittleEndian.PutUint16(out[20:22], 1) // Standard PCM format = 1
	binary.LittleEndian.PutUint16(out[22:24], channels)
	binary.LittleEndian.PutUint32(out[24:28], sampleRate)
	binary.LittleEndian.PutUint32(out[28:32], byteRate)
	binary.LittleEndian.PutUint16(out[32:34], blockAlign)
	binary.LittleEndian.PutUint16(out[34:36], bitsPerSample)
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], uint32(len(pcm)))
	copy(out[44:], pcm)

	durationSec := int((uint32(len(pcm)) + byteRate - 1) / byteRate)
	if durationSec < 1 {
		durationSec = 1
	}

	return out, durationSec, nil
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

func GetVoiceNotePath(seqID string) string {
	return filepath.Join(voiceHistoryDir(), "voice_"+seqID+".wav")
}

// SaveVoiceNote normalizes and saves base64 audio to the history folder, returning its duration in seconds.
func SaveVoiceNote(seqID, base64Audio string) (int, error) {
	rawAudio, err := base64.StdEncoding.DecodeString(base64Audio)
	if err != nil {
		return 0, err
	}
	cleanWav, durationSec, err := normalizeWAV(rawAudio)
	if err != nil {
		return 0, err
	}
	filePath := GetVoiceNotePath(seqID)
	if err := os.WriteFile(filePath, cleanWav, 0600); err != nil {
		return 0, err
	}
	return durationSec, nil
}

// PlayVoiceNoteByID plays a saved voice note and returns its duration in seconds.
func PlayVoiceNoteByID(seqID string) (int, error) {
	filePath := GetVoiceNotePath(seqID)
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return 0, fmt.Errorf("voice note #%s not found or expired", seqID)
	}
	_, durationSec, err := normalizeWAV(raw)
	if err != nil {
		durationSec = 1
	}
	if err := PlayAudio(filePath); err != nil {
		return 0, err
	}
	return durationSec, nil
}

func DeleteVoiceNote(seqID string) {
	if seqID == "" {
		return
	}
	_ = os.Remove(GetVoiceNotePath(seqID))
}

func PlayAudio(filePath string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		if path, err := exec.LookPath("paplay"); err == nil {
			cmd = exec.Command(path, filePath)
		} else if path, err := exec.LookPath("pw-play"); err == nil {
			cmd = exec.Command(path, filePath)
		} else if path, err := exec.LookPath("aplay"); err == nil {
			cmd = exec.Command(path, "-q", filePath)
		} else if path, err := exec.LookPath("ffplay"); err == nil {
			cmd = exec.Command(path, "-nodisp", "-autoexit", "-loglevel", "quiet", filePath)
		} else {
			return fmt.Errorf("no audio player found (install pipewire-pulse or alsa-utils)")
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