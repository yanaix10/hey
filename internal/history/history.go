package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const MessageTTL = 10 * time.Minute

type Message struct {
	Sender    string    `json:"sender"`
	Text      string    `json:"text"`
	Timestamp time.Time `json:"timestamp"`
}

func getHistoryDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	path := filepath.Join(dir, "hey", "history")
	os.MkdirAll(path, 0700)
	return path
}

func getPeerFile(peerUser string) string {
	return filepath.Join(getHistoryDir(), peerUser+".json")
}

// LoadAndClean returns valid messages and deletes ones older than 10 minutes
func LoadAndClean(peerUser string) []Message {
	filePath := getPeerFile(peerUser)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return []Message{}
	}

	var allMessages []Message
	if err := json.Unmarshal(data, &allMessages); err != nil {
		return []Message{}
	}

	var validMessages []Message
	cutoff := time.Now().Add(-MessageTTL)
	isDirty := false

	for _, msg := range allMessages {
		if msg.Timestamp.After(cutoff) {
			validMessages = append(validMessages, msg)
		} else {
			isDirty = true // We found expired messages
		}
	}

	// Overwrite file if old messages were removed
	if isDirty {
		if len(validMessages) == 0 {
			os.Remove(filePath)
		} else {
			newData, _ := json.MarshalIndent(validMessages, "", "  ")
			os.WriteFile(filePath, newData, 0600)
		}
	}

	return validMessages
}

// AppendMessage adds a new message and runs a cleanup
func AppendMessage(peerUser, sender, text string) {
	messages := LoadAndClean(peerUser)
	
	messages = append(messages, Message{
		Sender:    sender,
		Text:      text,
		Timestamp: time.Now(),
	})

	newData, _ := json.MarshalIndent(messages, "", "  ")
	os.WriteFile(getPeerFile(peerUser), newData, 0600)
}

// GarbageCollectAll scans all history files and deletes expired messages
func GarbageCollectAll() {
	dir := getHistoryDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			peerUser := strings.TrimSuffix(entry.Name(), ".json")
			LoadAndClean(peerUser) // This automatically deletes expired items
		}
	}
}