package encryption

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

var (
	documentShareSecretPattern = regexp.MustCompile(`^vdoc_share_[0-9a-f]{48}$`)
	documentShareHashPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func TestDocumentShareCapabilityGenerate_roundTrips100UniqueValues(t *testing.T) {
	// Given
	keyMaterial := "document-share-capability-test-key-material"
	keyring := mustEncryptionKeyring(t, DocumentShareCipherKID, keyMaterial, nil)
	seen := make(map[string]struct{}, 100)

	for index := range 100 {
		shareID := fmt.Sprintf("%032x", index+1)

		// When
		secret, record, err := GenerateDocumentShareCapability(shareID, keyring)

		// Then
		if err != nil {
			t.Fatalf("GenerateDocumentShareCapability() iteration %d error = %v", index, err)
		}
		if !documentShareSecretPattern.MatchString(secret) {
			t.Fatalf("generated capability iteration %d has invalid format", index)
		}
		if !documentShareHashPattern.MatchString(record.Hash) {
			t.Fatalf("generated capability iteration %d has invalid hash format", index)
		}
		if record.KID != DocumentShareCipherKID {
			t.Fatalf("generated capability iteration %d KID = %q, want %q", index, record.KID, DocumentShareCipherKID)
		}
		if bytes.Contains(record.Ciphertext, []byte(secret)) {
			t.Fatalf("generated capability iteration %d ciphertext contains plaintext", index)
		}
		if _, exists := seen[secret]; exists {
			t.Fatalf("generated capability iteration %d duplicated an earlier value", index)
		}
		seen[secret] = struct{}{}

		expectedHash := sha256.Sum256([]byte(secret))
		if record.Hash != hex.EncodeToString(expectedHash[:]) {
			t.Fatalf("generated capability iteration %d hash mismatch", index)
		}

		revealed, revealErr := RevealDocumentShareCapability(shareID, keyring, record)
		if revealErr != nil {
			t.Fatalf("RevealDocumentShareCapability() iteration %d error = %v", index, revealErr)
		}
		if revealed != secret {
			t.Fatalf("RevealDocumentShareCapability() iteration %d did not round-trip", index)
		}
		if !VerifyDocumentShareCapability(secret, record.Hash) {
			t.Fatalf("VerifyDocumentShareCapability() iteration %d rejected the matching hash", index)
		}
	}
}

func TestDocumentShareCapabilityGenerate_usesExactAADAndExistingCipherKeyDerivation(t *testing.T) {
	// Given
	shareID := "0123456789abcdef0123456789abcdef"
	keyMaterial := "  configured-mcp-token-cipher-material  "
	keyring := mustEncryptionKeyring(t, DocumentShareCipherKID, keyMaterial, nil)

	// When
	secret, record, err := GenerateDocumentShareCapability(shareID, keyring)
	if err != nil {
		t.Fatalf("GenerateDocumentShareCapability() error = %v", err)
	}

	// Then
	block, err := aes.NewCipher(deriveMCPTokenKey(keyMaterial))
	if err != nil {
		t.Fatalf("aes.NewCipher() error = %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("cipher.NewGCM() error = %v", err)
	}
	nonce := record.Ciphertext[:gcm.NonceSize()]
	sealed := record.Ciphertext[gcm.NonceSize():]
	exactAAD := []byte("vdoc.document-share\x00document-share-aes-gcm-v1\x00" + shareID)
	plaintext, err := gcm.Open(nil, nonce, sealed, exactAAD)
	if err != nil {
		t.Fatalf("ciphertext did not authenticate with the exact document-share AAD: %v", err)
	}
	if string(plaintext) != secret {
		t.Fatal("ciphertext authenticated but plaintext did not round-trip")
	}
	if _, err := gcm.Open(nil, nonce, sealed, []byte(MCPTokenCipherKID)); err == nil {
		t.Fatal("document-share ciphertext authenticated with MCP-token AAD")
	}
}

func TestDocumentShareCapabilityRotationPreservesSecretAndHash(t *testing.T) {
	const (
		shareID = "4123456789abcdef0123456789abcdef"
		oldKey  = "old-document-share-rotation-key-material"
		newKID  = "prod-2026-08"
		newKey  = "new-document-share-rotation-key-material"
	)
	legacy := mustEncryptionKeyring(t, DocumentShareCipherKID, oldKey, nil)
	secret, legacyRecord, err := GenerateDocumentShareCapability(shareID, legacy)
	if err != nil {
		t.Fatalf("GenerateDocumentShareCapability(legacy) error = %v", err)
	}

	// Operators only need to retain the historical MCP KID. NewKeyring adds
	// the old document-share KID alias because both formats used the same key.
	rotated := mustEncryptionKeyring(t, newKID, newKey, map[string]string{MCPTokenCipherKID: oldKey})
	newRecord, err := ReencryptDocumentShareCapability(shareID, rotated, legacyRecord)
	if err != nil {
		t.Fatalf("ReencryptDocumentShareCapability() error = %v", err)
	}
	if newRecord.KID != newKID || newRecord.Hash != legacyRecord.Hash || bytes.Equal(newRecord.Ciphertext, legacyRecord.Ciphertext) {
		t.Fatalf("rotated record = %+v, legacy = %+v", newRecord, legacyRecord)
	}
	revealed, err := RevealDocumentShareCapability(shareID, rotated, newRecord)
	if err != nil || revealed != secret {
		t.Fatalf("RevealDocumentShareCapability(rotated) = %q, %v", revealed, err)
	}
	activeOnly := mustEncryptionKeyring(t, newKID, newKey, nil)
	if revealed, err := RevealDocumentShareCapability(shareID, activeOnly, newRecord); err != nil || revealed != secret {
		t.Fatalf("active-only keyring could not reveal re-encrypted record: %q, %v", revealed, err)
	}
}

func TestVerifyDocumentShareCapability_rejectsMalformedOrMismatchedValues(t *testing.T) {
	// Given
	shareID := "1123456789abcdef0123456789abcdef"
	keyring := mustEncryptionKeyring(t, DocumentShareCipherKID, "hash-verification-key-material", nil)
	secret, record, err := GenerateDocumentShareCapability(shareID, keyring)
	if err != nil {
		t.Fatalf("GenerateDocumentShareCapability() error = %v", err)
	}
	wrongHash := "0" + record.Hash[1:]
	if wrongHash == record.Hash {
		wrongHash = "1" + record.Hash[1:]
	}

	tests := []struct {
		name   string
		secret string
		hash   string
	}{
		{name: "short secret", secret: secret[:len(secret)-1], hash: record.Hash},
		{name: "uppercase secret", secret: secret[:len("vdoc_share_")] + strings.ToUpper(secret[len("vdoc_share_"):]), hash: record.Hash},
		{name: "wrong prefix", secret: "x" + secret[1:], hash: record.Hash},
		{name: "uppercase hash", secret: secret, hash: strings.ToUpper(record.Hash)},
		{name: "wrong hash", secret: secret, hash: wrongHash},
		{name: "short hash", secret: secret, hash: record.Hash[:len(record.Hash)-1]},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// When
			verified := VerifyDocumentShareCapability(test.secret, test.hash)

			// Then
			if verified {
				t.Fatal("VerifyDocumentShareCapability() accepted invalid input")
			}
		})
	}
}

func TestRevealDocumentShareCapability_rejectsWrongBindingAndMalformedInputs(t *testing.T) {
	// Given
	shareIDA := "2123456789abcdef0123456789abcdef"
	shareIDB := "3123456789abcdef0123456789abcdef"
	keyMaterial := "row-binding-capability-key-material"
	keyring := mustEncryptionKeyring(t, DocumentShareCipherKID, keyMaterial, nil)
	secretA, recordA, err := GenerateDocumentShareCapability(shareIDA, keyring)
	if err != nil {
		t.Fatalf("GenerateDocumentShareCapability(A) error = %v", err)
	}
	_, recordB, err := GenerateDocumentShareCapability(shareIDB, keyring)
	if err != nil {
		t.Fatalf("GenerateDocumentShareCapability(B) error = %v", err)
	}
	mcpKeyring := mustEncryptionKeyring(t, MCPTokenCipherKID, keyMaterial, nil)
	mcpCiphertext, _, err := EncryptMCPToken(secretA, mcpKeyring)
	if err != nil {
		t.Fatalf("EncryptMCPToken() error = %v", err)
	}
	malformedPlaintext := "not-a-document-share-capability"
	malformedRecord := DocumentShareCapabilityRecord{
		Hash:       documentShareTestHash(malformedPlaintext),
		Ciphertext: sealDocumentShareTestValue(t, documentShareSealTestInput{shareID: shareIDA, keyMaterial: keyMaterial, plaintext: malformedPlaintext}),
		KID:        DocumentShareCipherKID,
	}

	wrongKID := recordA
	wrongKID.KID = MCPTokenCipherKID
	wrongHash := recordA
	wrongHash.Hash = documentShareTestHash("different-value")
	swappedCiphertext := recordA
	swappedCiphertext.Ciphertext = recordB.Ciphertext
	mcpSubstitution := recordA
	mcpSubstitution.Ciphertext = mcpCiphertext
	shortCiphertext := recordA
	shortCiphertext.Ciphertext = []byte("short")

	tests := []struct {
		name        string
		shareID     string
		keyMaterial string
		record      DocumentShareCapabilityRecord
	}{
		{name: "wrong key", shareID: shareIDA, keyMaterial: "different-key-material", record: recordA},
		{name: "wrong KID", shareID: shareIDA, keyMaterial: keyMaterial, record: wrongKID},
		{name: "wrong hash", shareID: shareIDA, keyMaterial: keyMaterial, record: wrongHash},
		{name: "wrong share", shareID: shareIDB, keyMaterial: keyMaterial, record: recordA},
		{name: "swapped ciphertext", shareID: shareIDA, keyMaterial: keyMaterial, record: swappedCiphertext},
		{name: "MCP ciphertext substitution", shareID: shareIDA, keyMaterial: keyMaterial, record: mcpSubstitution},
		{name: "short ciphertext", shareID: shareIDA, keyMaterial: keyMaterial, record: shortCiphertext},
		{name: "malformed plaintext", shareID: shareIDA, keyMaterial: keyMaterial, record: malformedRecord},
		{name: "malformed share ID", shareID: strings.ToUpper(shareIDA), keyMaterial: keyMaterial, record: recordA},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// When
			testKeyring := mustEncryptionKeyring(t, DocumentShareCipherKID, test.keyMaterial, nil)
			_, revealErr := RevealDocumentShareCapability(test.shareID, testKeyring, test.record)

			// Then
			if !errors.Is(revealErr, ErrInvalidDocumentShareCapability) {
				t.Fatalf("RevealDocumentShareCapability() error = %v, want generic capability error", revealErr)
			}
			for _, sensitive := range []string{secretA, keyMaterial, recordA.Hash, hex.EncodeToString(recordA.Ciphertext)} {
				if strings.Contains(revealErr.Error(), sensitive) {
					t.Fatal("RevealDocumentShareCapability() error exposed sensitive input")
				}
			}
		})
	}
}

func documentShareTestHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

type documentShareSealTestInput struct {
	shareID     string
	keyMaterial string
	plaintext   string
}

func sealDocumentShareTestValue(t *testing.T, input documentShareSealTestInput) []byte {
	t.Helper()
	block, err := aes.NewCipher(deriveMCPTokenKey(input.keyMaterial))
	if err != nil {
		t.Fatalf("aes.NewCipher() error = %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("cipher.NewGCM() error = %v", err)
	}
	nonce := bytes.Repeat([]byte{0x5a}, gcm.NonceSize())
	aad := []byte("vdoc.document-share\x00document-share-aes-gcm-v1\x00" + input.shareID)
	return gcm.Seal(append([]byte(nil), nonce...), nonce, []byte(input.plaintext), aad)
}
