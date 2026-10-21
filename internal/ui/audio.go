package ui

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

func prepareLinuxAudio() string {
	_ = exec.Command("wpctl", "set-mute", "@DEFAULT_AUDIO_SOURCE@", "0").Run()
	_ = exec.Command("wpctl", "set-volume", "@DEFAULT_AUDIO_SOURCE@", "1.0").Run()
	_ = exec.Command("wpctl", "set-mute", "@DEFAULT_AUDIO_SINK@", "0").Run()

	out, err := exec.Command("pactl", "list", "short", "sources").Output()
	if err != nil {
		return ""
	}

	var micSource string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			name := fields[1]
			if !strings.HasSuffix(name, ".monitor") {
				micSource = name
				if strings.Contains(name, "input") {
					break
				}
			}
		}
	}

	if micSource != "" {
		_ = exec.Command("pactl", "set-default-source", micSource).Run()
		_ = exec.Command("pactl", "set-source-mute", micSource, "0").Run()
		_ = exec.Command("pactl", "set-source-volume", micSource, "100%").Run()
	}
	_ = exec.Command("pactl", "set-sink-mute", "@DEFAULT_SINK@", "0").Run()
	return micSource
}

// StartRecording starts the OS native microphone recorder and waits until it is actively capturing.
func StartRecording() (*exec.Cmd, string, error) {
	file, err := os.CreateTemp("", "hey_voice_*.wav")
	if err != nil {
		return nil, "", err
	}
	filePath := file.Name()
	_ = file.Close()
	_ = os.Remove(filePath)

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		micSource := prepareLinuxAudio()

		if path, err := exec.LookPath("pw-record"); err == nil {
			args := []string{"--format=s16", "--rate=16000", "--channels=1"}
			if micSource != "" {
				args = append(args, "--target="+micSource)
			}
			args = append(args, filePath)
			cmd = exec.Command(path, args...)
		} else if path, err := exec.LookPath("parecord"); err == nil {
			args := []string{"--file-format=wav", "--format=s16le", "--rate=16000", "--channels=1"}
			if micSource != "" {
				args = append(args, "-d", micSource)
			}
			args = append(args, filePath)
			cmd = exec.Command(path, args...)
		} else if path, err := exec.LookPath("arecord"); err == nil {
			cmd = exec.Command(path, "-q", "-D", "default", "-f", "S16_LE", "-r", "16000", "-c", "1", "-t", "wav", filePath)
		} else if path, err := exec.LookPath("ffmpeg"); err == nil {
			input := "default"
			if micSource != "" {
				input = micSource
			}
			cmd = exec.Command(path, "-loglevel", "quiet", "-y", "-f", "pulse", "-i", input, "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", filePath)
		} else {
			return nil, "", fmt.Errorf("no recorder found (install pipewire, pulseaudio, or alsa-utils)")
		}

		if err := cmd.Start(); err != nil {
			return nil, "", err
		}
		return cmd, filePath, nil

	case "darwin":
		cmd = exec.Command("afrecord", "-d", "30", "-f", "WAVE", "-d", "LEI16@16000", "-c", "1", filePath)
		if err := cmd.Start(); err != nil {
			return nil, "", err
		}
		return cmd, filePath, nil

	case "windows":
		stopFile := filePath + ".stop"
		_ = os.Remove(stopFile)

		psScript := fmt.Sprintf(`
$code = @"
using System;
using System.IO;
using System.Runtime.InteropServices;
using System.Threading;

public class HeyWaveIn {
    [StructLayout(LayoutKind.Sequential)]
    public struct WAVEFORMATEX {
        public ushort wFormatTag;
        public ushort nChannels;
        public uint nSamplesPerSec;
        public uint nAvgBytesPerSec;
        public ushort nBlockAlign;
        public ushort wBitsPerSample;
        public ushort cbSize;
    }

    [StructLayout(LayoutKind.Sequential)]
    public class WAVEHDR {
        public IntPtr lpData;
        public uint dwBufferLength;
        public uint dwBytesRecorded;
        public IntPtr dwUser;
        public uint dwFlags;
        public uint dwLoops;
        public IntPtr lpNext;
        public IntPtr reserved;
    }

    [DllImport("winmm.dll")]
    public static extern int waveInOpen(out IntPtr hWaveIn, int uDeviceID, ref WAVEFORMATEX lpFormat, IntPtr dwCallback, IntPtr dwInstance, int dwFlags);
    [DllImport("winmm.dll")]
    public static extern int waveInPrepareHeader(IntPtr hWaveIn, IntPtr lpWaveInHdr, int uSize);
    [DllImport("winmm.dll")]
    public static extern int waveInAddBuffer(IntPtr hWaveIn, IntPtr lpWaveInHdr, int uSize);
    [DllImport("winmm.dll")]
    public static extern int waveInStart(IntPtr hWaveIn);
    [DllImport("winmm.dll")]
    public static extern int waveInReset(IntPtr hWaveIn);
    [DllImport("winmm.dll")]
    public static extern int waveInUnprepareHeader(IntPtr hWaveIn, IntPtr lpWaveInHdr, int uSize);
    [DllImport("winmm.dll")]
    public static extern int waveInClose(IntPtr hWaveIn);

    public static void Record(string outPath, string stopPath) {
        WAVEFORMATEX fmt = new WAVEFORMATEX();
        fmt.wFormatTag = 1;
        fmt.nChannels = 1;
        fmt.nSamplesPerSec = 16000;
        fmt.wBitsPerSample = 16;
        fmt.nBlockAlign = 2;
        fmt.nAvgBytesPerSec = 32000;
        fmt.cbSize = 0;

        IntPtr hWaveIn;
        int res = waveInOpen(out hWaveIn, -1, ref fmt, IntPtr.Zero, IntPtr.Zero, 0);
        if (res != 0) {
            Console.WriteLine("ERR:waveInOpen:" + res);
            Console.Out.Flush();
            return;
        }

        byte[] buf = new byte[32000 * 30];
        GCHandle hBuf = GCHandle.Alloc(buf, GCHandleType.Pinned);

        WAVEHDR hdr = new WAVEHDR();
        hdr.lpData = hBuf.AddrOfPinnedObject();
        hdr.dwBufferLength = (uint)buf.Length;

        GCHandle hHdr = GCHandle.Alloc(hdr, GCHandleType.Pinned);
        IntPtr pHdr = hHdr.AddrOfPinnedObject();
        int hdrSize = Marshal.SizeOf(typeof(WAVEHDR));

        waveInPrepareHeader(hWaveIn, pHdr, hdrSize);
        waveInAddBuffer(hWaveIn, pHdr, hdrSize);
        waveInStart(hWaveIn);

        Console.WriteLine("READY");
        Console.Out.Flush();

        for (int i = 0; i < 300; i++) {
            if (File.Exists(stopPath)) break;
            Thread.Sleep(100);
        }

        waveInReset(hWaveIn);
        int recorded = (int)hdr.dwBytesRecorded;
        waveInUnprepareHeader(hWaveIn, pHdr, hdrSize);
        hHdr.Free();
        hBuf.Free();
        waveInClose(hWaveIn);

        if (recorded > 0) {
            byte[] outBytes = new byte[recorded];
            Array.Copy(buf, 0, outBytes, 0, recorded);
            File.WriteAllBytes(outPath, outBytes);
        }
    }
}
"@
Add-Type -TypeDefinition $code
[HeyWaveIn]::Record('%s', '%s')
`, filePath, stopFile)

		cmd = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, "", err
		}
		if err := cmd.Start(); err != nil {
			return nil, "", err
		}

		statusCh := make(chan string, 1)
		go func() {
			r := bufio.NewReader(stdout)
			line, _ := r.ReadString('\n')
			statusCh <- strings.TrimSpace(line)
		}()

		select {
		case status := <-statusCh:
			if strings.HasPrefix(status, "ERR:") {
				_ = cmd.Process.Kill()
				return nil, "", fmt.Errorf("windows microphone error (%s) - check Windows Mic Privacy settings", status)
			}
		case <-time.After(4 * time.Second):
		}

		return cmd, filePath, nil

	default:
		return nil, "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// StopRecording stops the recorder, normalizes and boosts the WAV audio, and returns base64 + duration + peak%.
func StopRecording(cmd *exec.Cmd, filePath string) (string, int, int, error) {
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
		return "", 0, 0, fmt.Errorf("recorded audio is empty")
	}

	cleanWav, durationSec, peakPct, err := normalizeWAV(raw)
	if err != nil {
		return "", 0, 0, err
	}

	return base64.StdEncoding.EncodeToString(cleanWav), durationSec, peakPct, nil
}

func normalizeWAV(raw []byte) ([]byte, int, int, error) {
	audioFormat := uint16(1)
	channels := uint16(1)
	sampleRate := uint32(16000)
	bitsPerSample := uint16(16)
	var dataBytes []byte

	headerSearchLimit := len(raw)
	if headerSearchLimit > 1024 {
		headerSearchLimit = 1024
	}

	if bytes.HasPrefix(raw, []byte("RIFF")) {
		if fmtIdx := bytes.Index(raw[:headerSearchLimit], []byte("fmt ")); fmtIdx != -1 && fmtIdx+24 <= len(raw) {
			af := binary.LittleEndian.Uint16(raw[fmtIdx+8 : fmtIdx+10])
			ch := binary.LittleEndian.Uint16(raw[fmtIdx+10 : fmtIdx+12])
			sr := binary.LittleEndian.Uint32(raw[fmtIdx+12 : fmtIdx+16])
			bps := binary.LittleEndian.Uint16(raw[fmtIdx+22 : fmtIdx+24])

			if af == 0xFFFE && fmtIdx+34 <= len(raw) {
				af = binary.LittleEndian.Uint16(raw[fmtIdx+32 : fmtIdx+34])
			}
			audioFormat = af
			if ch >= 1 && ch <= 2 {
				channels = ch
			}
			if sr >= 8000 && sr <= 96000 {
				sampleRate = sr
			}
			if bps == 8 || bps == 16 || bps == 32 {
				bitsPerSample = bps
			}
		}
		if dataIdx := bytes.Index(raw[:headerSearchLimit], []byte("data")); dataIdx != -1 && dataIdx+8 <= len(raw) {
			dataBytes = raw[dataIdx+8:]
		} else if len(raw) > 44 {
			dataBytes = raw[44:]
		}
	} else {
		dataBytes = raw
	}

	if len(dataBytes) < 400 {
		return nil, 0, 0, fmt.Errorf("recorded audio is too short")
	}

	var samples []int16
	switch {
	case audioFormat == 3 && bitsPerSample == 32:
		step := 4 * int(channels)
		for i := 0; i+step <= len(dataBytes); i += step {
			f := math.Float32frombits(binary.LittleEndian.Uint32(dataBytes[i : i+4]))
			if f > 1.0 {
				f = 1.0
			} else if f < -1.0 {
				f = -1.0
			}
			samples = append(samples, int16(f*32767))
		}
	case bitsPerSample == 8:
		step := int(channels)
		for i := 0; i+step <= len(dataBytes); i += step {
			val := (int16(dataBytes[i]) - 128) * 256
			samples = append(samples, val)
		}
	default:
		step := 2 * int(channels)
		for i := 0; i+step <= len(dataBytes); i += step {
			val := int16(binary.LittleEndian.Uint16(dataBytes[i : i+2]))
			samples = append(samples, val)
		}
	}

	if len(samples) == 0 {
		return nil, 0, 0, fmt.Errorf("no audio samples captured")
	}

	var sum int64
	for _, s := range samples {
		sum += int64(s)
	}
	dcOffset := int16(sum / int64(len(samples)))

	var maxAmp int32
	for i := range samples {
		adj := int32(samples[i]) - int32(dcOffset)
		if adj > 32767 {
			adj = 32767
		} else if adj < -32768 {
			adj = -32768
		}
		samples[i] = int16(adj)
		if adj < 0 {
			adj = -adj
		}
		if adj > maxAmp {
			maxAmp = adj
		}
	}

	peakPct := int((maxAmp * 100) / 32767)
	if peakPct == 0 && maxAmp > 15 {
		peakPct = 1
	}

	if maxAmp > 25 && maxAmp < 24000 {
		gain := float64(28000) / float64(maxAmp)
		if gain > 16.0 {
			gain = 16.0
		}
		for i := range samples {
			boosted := float64(samples[i]) * gain
			if boosted > 32767 {
				boosted = 32767
			} else if boosted < -32768 {
				boosted = -32768
			}
			samples[i] = int16(boosted)
		}
	}

	pcmLen := len(samples) * 2
	byteRate := sampleRate * 2
	out := make([]byte, 44+pcmLen)
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(36+pcmLen))
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16)
	binary.LittleEndian.PutUint16(out[20:22], 1)
	binary.LittleEndian.PutUint16(out[22:24], 1)
	binary.LittleEndian.PutUint32(out[24:28], sampleRate)
	binary.LittleEndian.PutUint32(out[28:32], byteRate)
	binary.LittleEndian.PutUint16(out[32:34], 2)
	binary.LittleEndian.PutUint16(out[34:36], 16)
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], uint32(pcmLen))

	offset := 44
	for _, s := range samples {
		binary.LittleEndian.PutUint16(out[offset:offset+2], uint16(s))
		offset += 2
	}

	durationSec := int((uint32(pcmLen) + byteRate - 1) / byteRate)
	if durationSec < 1 {
		durationSec = 1
	}

	return out, durationSec, peakPct, nil
}

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

func SaveVoiceNote(seqID, base64Audio string) (int, error) {
	rawAudio, err := base64.StdEncoding.DecodeString(base64Audio)
	if err != nil {
		return 0, err
	}
	cleanWav, durationSec, _, err := normalizeWAV(rawAudio)
	if err != nil {
		return 0, err
	}
	filePath := GetVoiceNotePath(seqID)
	if err := os.WriteFile(filePath, cleanWav, 0600); err != nil {
		return 0, err
	}
	return durationSec, nil
}

func GetVoiceNoteDuration(seqID string) (int, error) {
	filePath := GetVoiceNotePath(seqID)
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return 0, fmt.Errorf("voice note #%s not found or expired", seqID)
	}
	_, durationSec, _, err := normalizeWAV(raw)
	if err != nil {
		return 1, nil
	}
	return durationSec, nil
}

func DeleteVoiceNote(seqID string) {
	if seqID == "" {
		return
	}
	_ = os.Remove(GetVoiceNotePath(seqID))
}

func PlayAudioSync(filePath string) error {
	switch runtime.GOOS {
	case "linux":
		_ = exec.Command("wpctl", "set-mute", "@DEFAULT_AUDIO_SINK@", "0").Run()
		_ = exec.Command("pactl", "set-sink-mute", "@DEFAULT_SINK@", "0").Run()

		candidates := [][]string{
			{"pw-play", filePath},
			{"paplay", filePath},
			{"aplay", "-D", "default", "-q", filePath},
			{"aplay", "-q", filePath},
			{"ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet", filePath},
		}
		var lastErr error
		for _, args := range candidates {
			if path, err := exec.LookPath(args[0]); err == nil {
				if err := exec.Command(path, args[1:]...).Run(); err == nil {
					return nil
				} else {
					lastErr = err
				}
			}
		}
		if lastErr != nil {
			return fmt.Errorf("audio playback failed: %v", lastErr)
		}
		return fmt.Errorf("no working audio player found")

	case "darwin":
		return exec.Command("afplay", filePath).Run()

	case "windows":
		psPlay := fmt.Sprintf(`(New-Object System.Media.SoundPlayer '%s').PlaySync()`, filePath)
		return exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psPlay).Run()

	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}