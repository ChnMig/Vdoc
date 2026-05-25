package encryption

import (
	"bytes"
	"testing"
)

func TestMCPTokenCiphertextRoundTripsWithoutPlaintext(t *testing.T) {
	token := "vdoc_secret_value"
	ciphertext, kid, err := EncryptMCPToken(token, "test-key-material")
	if err != nil {
		t.Fatalf("EncryptMCPToken() error = %v", err)
	}
	if kid != MCPTokenCipherKID {
		t.Fatalf("kid = %q, want %q", kid, MCPTokenCipherKID)
	}
	if bytes.Contains(ciphertext, []byte(token)) {
		t.Fatalf("ciphertext contains plaintext token %q", token)
	}
	decrypted, err := DecryptMCPToken(ciphertext, "test-key-material", kid)
	if err != nil {
		t.Fatalf("DecryptMCPToken() error = %v", err)
	}
	if decrypted != token {
		t.Fatalf("decrypted token = %q, want %q", decrypted, token)
	}
}
