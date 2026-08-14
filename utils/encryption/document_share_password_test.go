package encryption

import (
	"regexp"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestBcryptPasswordBytes_hashesAtCost12AndVerifies(t *testing.T) {
	// Given
	password := []byte("already-parsed-password")

	// When
	verifier, err := HashPasswordBytesWithBcrypt(password)

	// Then
	if err != nil {
		t.Fatalf("HashPasswordBytesWithBcrypt() error = %v", err)
	}
	if !regexp.MustCompile(`^[$]2[aby][$]12[$][./A-Za-z0-9]{53}$`).MatchString(verifier) {
		t.Fatal("HashPasswordBytesWithBcrypt() did not return an exact cost-12 bcrypt verifier")
	}
	cost, err := bcrypt.Cost([]byte(verifier))
	if err != nil {
		t.Fatalf("bcrypt.Cost() error = %v", err)
	}
	if cost != 12 {
		t.Fatalf("bcrypt cost = %d, want 12", cost)
	}
	if !VerifyBcryptPasswordBytes(password, verifier) {
		t.Fatal("VerifyBcryptPasswordBytes() rejected the matching password")
	}
	if VerifyBcryptPasswordBytes([]byte("different-password"), verifier) {
		t.Fatal("VerifyBcryptPasswordBytes() accepted a different password")
	}
	weakVerifier, err := bcrypt.GenerateFromPassword(password, 10)
	if err != nil {
		t.Fatalf("bcrypt.GenerateFromPassword() error = %v", err)
	}
	if VerifyBcryptPasswordBytes(password, string(weakVerifier)) {
		t.Fatal("VerifyBcryptPasswordBytes() accepted a non-policy bcrypt cost")
	}
}

func TestBcryptPasswordBytes_preservesExactBytes(t *testing.T) {
	// Given
	password := []byte(" leading-and-trailing-space ")

	// When
	verifier, err := HashPasswordBytesWithBcrypt(password)

	// Then
	if err != nil {
		t.Fatalf("HashPasswordBytesWithBcrypt() error = %v", err)
	}
	if !VerifyBcryptPasswordBytes(password, verifier) {
		t.Fatal("VerifyBcryptPasswordBytes() did not preserve exact password bytes")
	}
	if VerifyBcryptPasswordBytes([]byte("leading-and-trailing-space"), verifier) {
		t.Fatal("VerifyBcryptPasswordBytes() normalized password bytes")
	}
}
