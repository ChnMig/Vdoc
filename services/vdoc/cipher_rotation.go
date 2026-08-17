package vdoc

import (
	"fmt"

	"vdoc/utils/encryption"
)

// rotateCiphertextsLocked validates every persisted encrypted record against
// the configured keyring and rewrites historical KIDs with the active key. The
// caller persists the whole state only after all records have been validated,
// so an unknown KID or wrong key cannot create a partial database rotation.
func (s *Store) rotateCiphertextsLocked() (bool, error) {
	activeKID := s.cipherKeyring.ActiveKID()
	if activeKID == "" {
		return false, fmt.Errorf("active cipher KID is empty")
	}
	rotated := false

	for _, token := range s.tokens {
		if token == nil {
			continue
		}
		secret, err := encryption.DecryptMCPToken(token.TokenCiphertext, s.cipherKeyring, token.CipherKID)
		if err != nil {
			return false, fmt.Errorf("decrypt MCP token %s with KID %q: %w", token.ID, token.CipherKID, err)
		}
		if sha(secret) != token.TokenHash {
			return false, fmt.Errorf("MCP token %s ciphertext does not match its stored hash", token.ID)
		}
		if token.CipherKID == activeKID {
			continue
		}
		ciphertext, kid, err := encryption.EncryptMCPToken(secret, s.cipherKeyring)
		if err != nil {
			return false, fmt.Errorf("re-encrypt MCP token %s: %w", token.ID, err)
		}
		token.TokenCiphertext = ciphertext
		token.CipherKID = kid
		rotated = true
	}

	for _, provider := range s.aiProviders {
		if provider == nil {
			continue
		}
		apiKey, err := encryption.DecryptMCPToken(provider.APIKeyCiphertext, s.cipherKeyring, provider.CipherKID)
		if err != nil {
			return false, fmt.Errorf("decrypt AI provider %s key with KID %q: %w", provider.ID, provider.CipherKID, err)
		}
		if provider.CipherKID == activeKID {
			continue
		}
		ciphertext, kid, err := encryption.EncryptMCPToken(apiKey, s.cipherKeyring)
		if err != nil {
			return false, fmt.Errorf("re-encrypt AI provider %s key: %w", provider.ID, err)
		}
		provider.APIKeyCiphertext = ciphertext
		provider.CipherKID = kid
		rotated = true
	}

	for _, share := range s.shares {
		if share == nil {
			continue
		}
		record := encryption.DocumentShareCapabilityRecord{
			Hash:       share.TokenHash,
			Ciphertext: share.TokenCiphertext,
			KID:        share.CipherKID,
		}
		if share.CipherKID == activeKID {
			if _, err := encryption.RevealDocumentShareCapability(share.ID, s.cipherKeyring, record); err != nil {
				return false, fmt.Errorf("validate document share %s ciphertext with KID %q: %w", share.ID, share.CipherKID, err)
			}
			continue
		}
		rotatedRecord, err := encryption.ReencryptDocumentShareCapability(share.ID, s.cipherKeyring, record)
		if err != nil {
			return false, fmt.Errorf("re-encrypt document share %s with KID %q: %w", share.ID, share.CipherKID, err)
		}
		share.TokenCiphertext = rotatedRecord.Ciphertext
		share.CipherKID = rotatedRecord.KID
		rotated = true
	}

	return rotated, nil
}
