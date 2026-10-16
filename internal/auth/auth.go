package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
)

// GeneratePIN creates a cryptographically secure 6-digit numeric PIN
func GeneratePIN() (string, error) {
	var n uint32
	err := binary.Read(rand.Reader, binary.BigEndian, &n)
	if err != nil {
		return "", err
	}
	pin := fmt.Sprintf("%06d", n%1000000)
	return pin, nil
}

// DeriveKey derives a 32-byte AES key from the user PIN and a network salt
func DeriveKey(pin string, salt []byte) []byte {
	hash := sha256.Sum256(append([]byte(pin), salt...))
	return hash[:]
}

// Encrypt encrypts a message using AES-GCM
func Encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	// Append nonce to ciphertext for transmission
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt unpacks and decrypts an AES-GCM payload
func Decrypt(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ct, nil)
}