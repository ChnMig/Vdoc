package authentication

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"vdoc/config"
)

const unlockProofTestKey = "document-share-unlock-test-key-material-32chars"

func TestDocumentShareUnlockProof_roundTripsWithExactContract(t *testing.T) {
	// Given
	setDocumentShareUnlockTestKey(t, unlockProofTestKey)
	shareID := "4123456789abcdef0123456789abcdef"
	now := time.Date(2026, time.July, 20, 12, 30, 45, 987654321, time.FixedZone("test", 8*60*60))

	// When
	proof, expiresAt, err := SignDocumentShareUnlockProof(shareID, now, nil)

	// Then
	if err != nil {
		t.Fatalf("SignDocumentShareUnlockProof() error = %v", err)
	}
	if !regexp.MustCompile(`^vdoc_share_unlock_[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`).MatchString(proof) {
		t.Fatal("SignDocumentShareUnlockProof() returned an invalid proof format")
	}
	expectedExpiry := time.Unix(now.Unix()+900, 0).UTC()
	if !expiresAt.Equal(expectedExpiry) {
		t.Fatalf("expiresAt = %s, want %s", expiresAt, expectedExpiry)
	}
	header, claims := decodeDocumentShareUnlockTestProof(t, proof)
	if header != (documentShareUnlockTestHeader{Alg: "HS256", Type: "VdocShareUnlock", KID: "document-share-unlock-v1"}) {
		t.Fatalf("protected header = %+v, want exact unlock header", header)
	}
	if claims != (documentShareUnlockTestClaims{Subject: shareID, IssuedAt: now.Unix(), ExpiresAt: expectedExpiry.Unix()}) {
		t.Fatalf("claims = %+v, want exact unlock claims", claims)
	}
	if err := ValidateDocumentShareUnlockProof(proof, shareID, now); err != nil {
		t.Fatalf("ValidateDocumentShareUnlockProof() error = %v", err)
	}
}

func TestDocumentShareUnlockProof_capsExpiryAtShareExpiry(t *testing.T) {
	// Given
	setDocumentShareUnlockTestKey(t, unlockProofTestKey)
	shareID := "5123456789abcdef0123456789abcdef"
	now := time.Unix(1_800_000_000, 0).UTC()
	shareExpiresAt := now.Add(125 * time.Second)

	// When
	proof, expiresAt, err := SignDocumentShareUnlockProof(shareID, now, &shareExpiresAt)

	// Then
	if err != nil {
		t.Fatalf("SignDocumentShareUnlockProof() error = %v", err)
	}
	if !expiresAt.Equal(shareExpiresAt) {
		t.Fatalf("expiresAt = %s, want share expiry %s", expiresAt, shareExpiresAt)
	}
	if err := ValidateDocumentShareUnlockProof(proof, shareID, expiresAt.Add(-time.Second)); err != nil {
		t.Fatalf("ValidateDocumentShareUnlockProof() before expiry error = %v", err)
	}
	if err := ValidateDocumentShareUnlockProof(proof, shareID, expiresAt); !errors.Is(err, ErrInvalidDocumentShareUnlockProof) {
		t.Fatalf("ValidateDocumentShareUnlockProof() at expiry error = %v, want generic proof error", err)
	}
}

func TestDocumentShareUnlockProof_rejectsNonPositiveLifetime(t *testing.T) {
	// Given
	setDocumentShareUnlockTestKey(t, unlockProofTestKey)
	shareID := "6123456789abcdef0123456789abcdef"
	now := time.Unix(1_800_000_000, 500_000_000).UTC()
	shareExpiresAt := time.Unix(now.Unix(), 900_000_000).UTC()

	// When
	_, _, err := SignDocumentShareUnlockProof(shareID, now, &shareExpiresAt)

	// Then
	if !errors.Is(err, ErrInvalidDocumentShareUnlockProof) {
		t.Fatalf("SignDocumentShareUnlockProof() error = %v, want generic proof error", err)
	}
}

func TestDocumentShareUnlockProof_usesAKeySeparatedFromAccountJWTs(t *testing.T) {
	// Given
	setDocumentShareUnlockTestKey(t, unlockProofTestKey)
	shareID := "7123456789abcdef0123456789abcdef"
	now := time.Unix(1_800_000_000, 0).UTC()
	proof, _, err := SignDocumentShareUnlockProof(shareID, now, nil)
	if err != nil {
		t.Fatalf("SignDocumentShareUnlockProof() error = %v", err)
	}
	accountToken, err := JWTIssue(map[string]any{"user_id": "account-user"})
	if err != nil {
		t.Fatalf("JWTIssue() error = %v", err)
	}

	// When
	_, accountParseErr := JWTDecrypt(strings.TrimPrefix(proof, DocumentShareUnlockProofPrefix))
	accountSubstitutionErr := ValidateDocumentShareUnlockProof(DocumentShareUnlockProofPrefix+accountToken, shareID, now)

	// Then
	if accountParseErr == nil {
		t.Fatal("account JWT validation accepted an unlock proof suffix")
	}
	if !errors.Is(accountSubstitutionErr, ErrInvalidDocumentShareUnlockProof) {
		t.Fatalf("unlock proof validation error = %v, want generic proof error", accountSubstitutionErr)
	}
}

func TestDocumentShareUnlockProof_rejectsWrongKeyShareAndSegmentSwap(t *testing.T) {
	// Given
	setDocumentShareUnlockTestKey(t, unlockProofTestKey)
	shareIDA := "8123456789abcdef0123456789abcdef"
	shareIDB := "9123456789abcdef0123456789abcdef"
	now := time.Unix(1_800_000_000, 0).UTC()
	proofA, _, err := SignDocumentShareUnlockProof(shareIDA, now, nil)
	if err != nil {
		t.Fatalf("SignDocumentShareUnlockProof(A) error = %v", err)
	}
	proofB, _, err := SignDocumentShareUnlockProof(shareIDB, now, nil)
	if err != nil {
		t.Fatalf("SignDocumentShareUnlockProof(B) error = %v", err)
	}
	segmentsA := strings.Split(strings.TrimPrefix(proofA, DocumentShareUnlockProofPrefix), ".")
	segmentsB := strings.Split(strings.TrimPrefix(proofB, DocumentShareUnlockProofPrefix), ".")
	swapped := DocumentShareUnlockProofPrefix + strings.Join([]string{segmentsA[0], segmentsB[1], segmentsA[2]}, ".")

	// When
	wrongShareErr := ValidateDocumentShareUnlockProof(proofA, shareIDB, now)
	swapErr := ValidateDocumentShareUnlockProof(swapped, shareIDA, now)
	config.JWTKey = "different-document-share-unlock-test-key"
	wrongKeyErr := ValidateDocumentShareUnlockProof(proofA, shareIDA, now)

	// Then
	for name, validationErr := range map[string]error{
		"wrong share":  wrongShareErr,
		"segment swap": swapErr,
		"wrong key":    wrongKeyErr,
	} {
		if !errors.Is(validationErr, ErrInvalidDocumentShareUnlockProof) {
			t.Fatalf("%s error = %v, want generic proof error", name, validationErr)
		}
	}
}

func TestDocumentShareUnlockProof_errorsDoNotExposeSensitiveInput(t *testing.T) {
	// Given
	setDocumentShareUnlockTestKey(t, unlockProofTestKey)
	shareID := "a123456789abcdef0123456789abcdef"
	now := time.Unix(1_800_000_000, 0).UTC()
	proof, _, err := SignDocumentShareUnlockProof(shareID, now, nil)
	if err != nil {
		t.Fatalf("SignDocumentShareUnlockProof() error = %v", err)
	}
	config.JWTKey = "different-document-share-unlock-test-key"

	// When
	validationErr := ValidateDocumentShareUnlockProof(proof, shareID, now)

	// Then
	if !errors.Is(validationErr, ErrInvalidDocumentShareUnlockProof) {
		t.Fatalf("ValidateDocumentShareUnlockProof() error = %v, want generic proof error", validationErr)
	}
	for _, sensitive := range []string{proof, shareID, unlockProofTestKey, config.JWTKey} {
		if strings.Contains(validationErr.Error(), sensitive) {
			t.Fatal("ValidateDocumentShareUnlockProof() error exposed sensitive input")
		}
	}
}

type documentShareUnlockTestHeader struct {
	Alg  string `json:"alg"`
	Type string `json:"typ"`
	KID  string `json:"kid"`
}

type documentShareUnlockTestClaims struct {
	Subject   string `json:"sub"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

func setDocumentShareUnlockTestKey(t *testing.T, key string) {
	t.Helper()
	previous := config.JWTKey
	config.JWTKey = key
	t.Cleanup(func() {
		config.JWTKey = previous
	})
}

func decodeDocumentShareUnlockTestProof(t *testing.T, proof string) (documentShareUnlockTestHeader, documentShareUnlockTestClaims) {
	t.Helper()
	segments := strings.Split(strings.TrimPrefix(proof, DocumentShareUnlockProofPrefix), ".")
	if len(segments) != 3 {
		t.Fatalf("proof segment count = %d, want 3", len(segments))
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(segments[0])
	if err != nil {
		t.Fatalf("decode protected header: %v", err)
	}
	claimsJSON, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	var header documentShareUnlockTestHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		t.Fatalf("unmarshal protected header: %v", err)
	}
	var claims documentShareUnlockTestClaims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	return header, claims
}

func signDocumentShareUnlockTestJWS(header, payload string, key []byte) string {
	headerSegment := base64.RawURLEncoding.EncodeToString([]byte(header))
	payloadSegment := base64.RawURLEncoding.EncodeToString([]byte(payload))
	signingInput := headerSegment + "." + payloadSegment
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(signingInput))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return DocumentShareUnlockProofPrefix + signingInput + "." + signature
}
