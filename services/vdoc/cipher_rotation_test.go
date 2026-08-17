package vdoc

import (
	"errors"
	"testing"

	"vdoc/utils/encryption"
)

func TestRotateCiphertextsLockedReencryptsTokensProvidersAndShares(t *testing.T) {
	const (
		oldKey      = "old-cipher-key-material-0123456789"
		newKID      = "prod-2026-08"
		newKey      = "new-cipher-key-material-9876543210"
		tokenSecret = "vdoc_0123456789abcdef0123456789abcdef0123456789abcdef"
		providerKey = "provider-secret-key"
		shareID     = "0123456789abcdef0123456789abcdef"
	)
	legacyTokenKeyring := mustServiceKeyring(t, encryption.MCPTokenCipherKID, oldKey, nil)
	tokenCiphertext, tokenKID, err := encryption.EncryptMCPToken(tokenSecret, legacyTokenKeyring)
	if err != nil {
		t.Fatalf("encrypt legacy token: %v", err)
	}
	providerCiphertext, providerKID, err := encryption.EncryptMCPToken(providerKey, legacyTokenKeyring)
	if err != nil {
		t.Fatalf("encrypt legacy provider key: %v", err)
	}
	legacyShareKeyring := mustServiceKeyring(t, encryption.DocumentShareCipherKID, oldKey, nil)
	shareSecret, shareRecord, err := encryption.GenerateDocumentShareCapability(shareID, legacyShareKeyring)
	if err != nil {
		t.Fatalf("generate legacy share: %v", err)
	}

	store := NewStore()
	store.tokens["token-1"] = &MCPToken{
		ID: "token-1", TokenHash: sha(tokenSecret), TokenCiphertext: tokenCiphertext, CipherKID: tokenKID,
	}
	store.aiProviders["system"] = &AIProviderConfig{
		ID: "provider-1", APIKeyCiphertext: providerCiphertext, CipherKID: providerKID,
	}
	store.shares[shareID] = &DocumentShare{
		ID: shareID, TokenHash: shareRecord.Hash, TokenCiphertext: shareRecord.Ciphertext, CipherKID: shareRecord.KID,
	}
	store.cipherKeyring = mustServiceKeyring(t, newKID, newKey, map[string]string{encryption.MCPTokenCipherKID: oldKey})

	rotated, err := store.rotateCiphertextsLocked()
	if err != nil {
		t.Fatalf("rotateCiphertextsLocked() error = %v", err)
	}
	if !rotated {
		t.Fatal("rotateCiphertextsLocked() did not report changed ciphertexts")
	}
	if store.tokens["token-1"].CipherKID != newKID || store.aiProviders["system"].CipherKID != newKID || store.shares[shareID].CipherKID != newKID {
		t.Fatalf("rotated KIDs = token:%q provider:%q share:%q", store.tokens["token-1"].CipherKID, store.aiProviders["system"].CipherKID, store.shares[shareID].CipherKID)
	}

	activeOnly := mustServiceKeyring(t, newKID, newKey, nil)
	if got, err := encryption.DecryptMCPToken(store.tokens["token-1"].TokenCiphertext, activeOnly, newKID); err != nil || got != tokenSecret {
		t.Fatalf("decrypt rotated token = %q, %v", got, err)
	}
	if got, err := encryption.DecryptMCPToken(store.aiProviders["system"].APIKeyCiphertext, activeOnly, newKID); err != nil || got != providerKey {
		t.Fatalf("decrypt rotated provider key = %q, %v", got, err)
	}
	if got, err := encryption.RevealDocumentShareCapability(shareID, activeOnly, encryption.DocumentShareCapabilityRecord{
		Hash: store.shares[shareID].TokenHash, Ciphertext: store.shares[shareID].TokenCiphertext, KID: newKID,
	}); err != nil || got != shareSecret {
		t.Fatalf("reveal rotated share = %q, %v", got, err)
	}

	rotated, err = store.rotateCiphertextsLocked()
	if err != nil || rotated {
		t.Fatalf("second rotateCiphertextsLocked() = %t, %v; want idempotent false, nil", rotated, err)
	}
}

func TestRotateCiphertextsLockedRejectsUnknownKIDAndWrongActiveKey(t *testing.T) {
	store := NewStore()
	store.tokens["token-unknown"] = &MCPToken{
		ID: "token-unknown", TokenHash: sha("secret"), TokenCiphertext: []byte("ciphertext"), CipherKID: "missing-kid",
	}
	store.cipherKeyring = mustServiceKeyring(t, "active", "active-key-material-0123456789012345", nil)
	if _, err := store.rotateCiphertextsLocked(); !errors.Is(err, encryption.ErrUnknownCipherKID) {
		t.Fatalf("rotateCiphertextsLocked(unknown KID) error = %v, want ErrUnknownCipherKID", err)
	}

	correct := mustServiceKeyring(t, "active", "correct-key-material-01234567890123", nil)
	ciphertext, kid, err := encryption.EncryptMCPToken("secret", correct)
	if err != nil {
		t.Fatalf("encrypt active token: %v", err)
	}
	store.tokens["token-unknown"] = &MCPToken{
		ID: "token-active", TokenHash: sha("secret"), TokenCiphertext: ciphertext, CipherKID: kid,
	}
	store.cipherKeyring = mustServiceKeyring(t, "active", "wrong-key-material-012345678901234", nil)
	if _, err := store.rotateCiphertextsLocked(); err == nil {
		t.Fatal("rotateCiphertextsLocked() accepted a reused KID with the wrong key")
	}
}

func mustServiceKeyring(t *testing.T, activeKID, activeKey string, historical map[string]string) encryption.Keyring {
	t.Helper()
	keyring, err := encryption.NewKeyring(activeKID, activeKey, historical)
	if err != nil {
		t.Fatalf("NewKeyring() error = %v", err)
	}
	return keyring
}
