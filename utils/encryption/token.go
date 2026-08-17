package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const MCPTokenCipherKID = "local-aes-gcm-v1"

var (
	ErrUnknownCipherKID = errors.New("unknown cipher KID")
	cipherKIDPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
)

// Keyring keeps one active write key and zero or more read-only historical
// keys. Ciphertext records carry the KID, so rotating the active key does not
// make existing MCP tokens, AI provider keys, or document shares unreadable.
// The key material is intentionally private and is never formatted or logged.
type Keyring struct {
	activeKID string
	keys      map[string]string
}

func NewKeyring(activeKID, activeKey string, historicalKeys map[string]string) (Keyring, error) {
	activeKID = strings.TrimSpace(activeKID)
	if !cipherKIDPattern.MatchString(activeKID) {
		return Keyring{}, fmt.Errorf("invalid active cipher KID %q", activeKID)
	}
	keys := make(map[string]string, len(historicalKeys)+2)
	keys[activeKID] = activeKey
	for kid, key := range historicalKeys {
		normalizedKID := strings.TrimSpace(kid)
		if !cipherKIDPattern.MatchString(normalizedKID) {
			return Keyring{}, fmt.Errorf("invalid historical cipher KID %q", kid)
		}
		if normalizedKID == activeKID {
			return Keyring{}, fmt.Errorf("active cipher KID %q must not also appear in the historical keyring", activeKID)
		}
		if _, exists := keys[normalizedKID]; exists {
			return Keyring{}, fmt.Errorf("duplicate historical cipher KID %q", normalizedKID)
		}
		keys[normalizedKID] = key
	}

	// Before keyrings existed, token/provider records and document-share
	// records used different algorithm labels with the same configured key.
	// This alias makes a single historical local-aes-gcm-v1 entry sufficient
	// to read both record types during rotation.
	if legacyKey, ok := keys[MCPTokenCipherKID]; ok {
		if _, exists := keys[DocumentShareCipherKID]; !exists {
			keys[DocumentShareCipherKID] = legacyKey
		}
	}
	return Keyring{activeKID: activeKID, keys: keys}, nil
}

func (k Keyring) ActiveKID() string { return k.activeKID }

func (k Keyring) keyFor(kid string) (string, error) {
	if !cipherKIDPattern.MatchString(kid) {
		return "", fmt.Errorf("%w %q", ErrUnknownCipherKID, kid)
	}
	key, ok := k.keys[kid]
	if !ok {
		return "", fmt.Errorf("%w %q", ErrUnknownCipherKID, kid)
	}
	return key, nil
}

func EncryptMCPToken(token string, keyring Keyring) ([]byte, string, error) {
	kid := keyring.ActiveKID()
	keyMaterial, err := keyring.keyFor(kid)
	if err != nil {
		return nil, "", err
	}
	return encryptAESGCM([]byte(token), keyMaterial, []byte(kid), kid)
}

func DecryptMCPToken(ciphertext []byte, keyring Keyring, cipherKID string) (string, error) {
	keyMaterial, err := keyring.keyFor(cipherKID)
	if err != nil {
		return "", err
	}
	plaintext, err := decryptAESGCM(ciphertext, keyMaterial, []byte(cipherKID))
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func encryptAESGCM(plaintext []byte, keyMaterial string, aad []byte, kid string) ([]byte, string, error) {
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
	ciphertext := gcm.Seal(append([]byte(nil), nonce...), nonce, plaintext, aad)
	return ciphertext, kid, nil
}

func decryptAESGCM(ciphertext []byte, keyMaterial string, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(deriveMCPTokenKey(keyMaterial))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, fmt.Errorf("ciphertext is too short")
	}
	nonce := ciphertext[:gcm.NonceSize()]
	sealed := ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, sealed, aad)
}

func deriveMCPTokenKey(keyMaterial string) []byte {
	keyMaterial = strings.TrimSpace(keyMaterial)
	if keyMaterial == "" {
		keyMaterial = "vdoc-local-mcp-token-key"
	}
	sum := sha256.Sum256([]byte(keyMaterial))
	return sum[:]
}
