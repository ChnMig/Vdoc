package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
)

const MCPTokenCipherKID = "local-aes-gcm-v1"

func EncryptMCPToken(token, keyMaterial string) ([]byte, string, error) {
	block, err := aes.NewCipher(deriveMCPTokenKey(keyMaterial))
	if err != nil {
		return nil, "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, "", err
	}
	ciphertext := gcm.Seal(append([]byte(nil), nonce...), nonce, []byte(token), []byte(MCPTokenCipherKID))
	return ciphertext, MCPTokenCipherKID, nil
}

func DecryptMCPToken(ciphertext []byte, keyMaterial, cipherKID string) (string, error) {
	if cipherKID != MCPTokenCipherKID {
		return "", fmt.Errorf("unsupported token cipher kid %q", cipherKID)
	}
	block, err := aes.NewCipher(deriveMCPTokenKey(keyMaterial))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return "", fmt.Errorf("token ciphertext is too short")
	}
	nonce := ciphertext[:gcm.NonceSize()]
	sealed := ciphertext[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, sealed, []byte(cipherKID))
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func deriveMCPTokenKey(keyMaterial string) []byte {
	keyMaterial = strings.TrimSpace(keyMaterial)
	if keyMaterial == "" {
		keyMaterial = "vdoc-local-mcp-token-key"
	}
	sum := sha256.Sum256([]byte(keyMaterial))
	return sum[:]
}
