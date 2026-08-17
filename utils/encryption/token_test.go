package encryption

import (
	"bytes"
	"errors"
	"testing"
)

func TestMCPTokenCiphertextRoundTripsWithoutPlaintext(t *testing.T) {
	keyring := mustEncryptionKeyring(t, MCPTokenCipherKID, "test-key-material", nil)
	token := "vdoc_secret_value"
	ciphertext, kid, err := EncryptMCPToken(token, keyring)
	if err != nil {
		t.Fatalf("EncryptMCPToken() error = %v", err)
	}
	if kid != MCPTokenCipherKID {
		t.Fatalf("kid = %q, want %q", kid, MCPTokenCipherKID)
	}
	if bytes.Contains(ciphertext, []byte(token)) {
		t.Fatalf("ciphertext contains plaintext token %q", token)
	}
	decrypted, err := DecryptMCPToken(ciphertext, keyring, kid)
	if err != nil {
		t.Fatalf("DecryptMCPToken() error = %v", err)
	}
	if decrypted != token {
		t.Fatalf("decrypted token = %q, want %q", decrypted, token)
	}
}

func TestKeyringRotationReadsOldCiphertextAndWritesOnlyActiveKID(t *testing.T) {
	const (
		oldKey = "old-key-material-for-vdoc-rotation"
		newKID = "prod-2026-08"
		newKey = "new-key-material-for-vdoc-rotation"
		token  = "vdoc_rotation_secret"
	)
	legacy := mustEncryptionKeyring(t, MCPTokenCipherKID, oldKey, nil)
	oldCiphertext, oldKID, err := EncryptMCPToken(token, legacy)
	if err != nil {
		t.Fatalf("EncryptMCPToken(legacy) error = %v", err)
	}

	rotated := mustEncryptionKeyring(t, newKID, newKey, map[string]string{MCPTokenCipherKID: oldKey})
	decrypted, err := DecryptMCPToken(oldCiphertext, rotated, oldKID)
	if err != nil || decrypted != token {
		t.Fatalf("DecryptMCPToken(old with rotated keyring) = %q, %v", decrypted, err)
	}
	newCiphertext, writtenKID, err := EncryptMCPToken(token, rotated)
	if err != nil {
		t.Fatalf("EncryptMCPToken(rotated) error = %v", err)
	}
	if writtenKID != newKID {
		t.Fatalf("rotated write KID = %q, want %q", writtenKID, newKID)
	}
	if _, err := DecryptMCPToken(newCiphertext, legacy, writtenKID); !errors.Is(err, ErrUnknownCipherKID) {
		t.Fatalf("legacy keyring decrypt new KID error = %v, want ErrUnknownCipherKID", err)
	}
}

func TestKeyringRejectsAmbiguousOrUnknownKIDs(t *testing.T) {
	if _, err := NewKeyring("active", "active-key", map[string]string{"active": "different-key"}); err == nil {
		t.Fatal("NewKeyring() accepted active KID in historical keys")
	}
	keyring := mustEncryptionKeyring(t, "active", "active-key", nil)
	if _, err := DecryptMCPToken([]byte("ciphertext"), keyring, "missing"); !errors.Is(err, ErrUnknownCipherKID) {
		t.Fatalf("DecryptMCPToken(unknown KID) error = %v, want ErrUnknownCipherKID", err)
	}
}

func mustEncryptionKeyring(t *testing.T, activeKID, activeKey string, historical map[string]string) Keyring {
	t.Helper()
	keyring, err := NewKeyring(activeKID, activeKey, historical)
	if err != nil {
		t.Fatalf("NewKeyring() error = %v", err)
	}
	return keyring
}
