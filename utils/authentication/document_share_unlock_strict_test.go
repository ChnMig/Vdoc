package authentication

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func TestDocumentShareUnlockProof_rejectsMalformedHeadersClaimsAndTimes(t *testing.T) {
	// Given
	setDocumentShareUnlockTestKey(t, unlockProofTestKey)
	shareID := "b123456789abcdef0123456789abcdef"
	now := time.Unix(1_800_000_000, 0).UTC()
	keyMAC := hmac.New(sha256.New, []byte(unlockProofTestKey))
	_, _ = keyMAC.Write([]byte("vdoc.document-share.unlock-proof.v1"))
	key := keyMAC.Sum(nil)
	exactHeader := `{"alg":"HS256","typ":"VdocShareUnlock","kid":"document-share-unlock-v1"}`
	exactPayload := fmt.Sprintf(`{"sub":%q,"iat":%d,"exp":%d}`, shareID, now.Unix(), now.Unix()+900)
	rawKeyProof := signDocumentShareUnlockTestJWS(exactHeader, exactPayload, []byte(unlockProofTestKey))

	tests := []struct {
		name  string
		proof string
	}{
		{name: "missing prefix", proof: strings.TrimPrefix(signDocumentShareUnlockTestJWS(exactHeader, exactPayload, key), DocumentShareUnlockProofPrefix)},
		{name: "too few segments", proof: DocumentShareUnlockProofPrefix + "one.two"},
		{name: "too many segments", proof: DocumentShareUnlockProofPrefix + "one.two.three.four"},
		{name: "padded base64", proof: DocumentShareUnlockProofPrefix + "e30=.e30.e30"},
		{name: "wrong algorithm", proof: signDocumentShareUnlockTestJWS(`{"alg":"HS512","typ":"VdocShareUnlock","kid":"document-share-unlock-v1"}`, exactPayload, key)},
		{name: "wrong type", proof: signDocumentShareUnlockTestJWS(`{"alg":"HS256","typ":"JWT","kid":"document-share-unlock-v1"}`, exactPayload, key)},
		{name: "wrong KID", proof: signDocumentShareUnlockTestJWS(`{"alg":"HS256","typ":"VdocShareUnlock","kid":"other"}`, exactPayload, key)},
		{name: "extra header", proof: signDocumentShareUnlockTestJWS(`{"alg":"HS256","typ":"VdocShareUnlock","kid":"document-share-unlock-v1","extra":"x"}`, exactPayload, key)},
		{name: "duplicate header", proof: signDocumentShareUnlockTestJWS(`{"alg":"HS256","alg":"HS256","typ":"VdocShareUnlock","kid":"document-share-unlock-v1"}`, exactPayload, key)},
		{name: "missing subject", proof: signDocumentShareUnlockTestJWS(exactHeader, fmt.Sprintf(`{"iat":%d,"exp":%d}`, now.Unix(), now.Unix()+900), key)},
		{name: "extra claim", proof: signDocumentShareUnlockTestJWS(exactHeader, fmt.Sprintf(`{"sub":%q,"iat":%d,"exp":%d,"aud":"x"}`, shareID, now.Unix(), now.Unix()+900), key)},
		{name: "duplicate subject", proof: signDocumentShareUnlockTestJWS(exactHeader, fmt.Sprintf(`{"sub":%q,"sub":%q,"iat":%d,"exp":%d}`, shareID, shareID, now.Unix(), now.Unix()+900), key)},
		{name: "string issued-at", proof: signDocumentShareUnlockTestJWS(exactHeader, fmt.Sprintf(`{"sub":%q,"iat":"%d","exp":%d}`, shareID, now.Unix(), now.Unix()+900), key)},
		{name: "fractional expiry", proof: signDocumentShareUnlockTestJWS(exactHeader, fmt.Sprintf(`{"sub":%q,"iat":%d,"exp":%d.5}`, shareID, now.Unix(), now.Unix()+900), key)},
		{name: "future issued-at", proof: signDocumentShareUnlockTestJWS(exactHeader, fmt.Sprintf(`{"sub":%q,"iat":%d,"exp":%d}`, shareID, now.Unix()+1, now.Unix()+2), key)},
		{name: "expired", proof: signDocumentShareUnlockTestJWS(exactHeader, fmt.Sprintf(`{"sub":%q,"iat":%d,"exp":%d}`, shareID, now.Unix()-900, now.Unix()), key)},
		{name: "non-positive lifetime", proof: signDocumentShareUnlockTestJWS(exactHeader, fmt.Sprintf(`{"sub":%q,"iat":%d,"exp":%d}`, shareID, now.Unix(), now.Unix()), key)},
		{name: "lifetime over 900 seconds", proof: signDocumentShareUnlockTestJWS(exactHeader, fmt.Sprintf(`{"sub":%q,"iat":%d,"exp":%d}`, shareID, now.Unix(), now.Unix()+901), key)},
		{name: "overflowed lifetime", proof: signDocumentShareUnlockTestJWS(exactHeader, fmt.Sprintf(`{"sub":%q,"iat":%d,"exp":%d}`, shareID, int64(math.MinInt64), int64(math.MaxInt64)), key)},
		{name: "raw account key", proof: rawKeyProof},
		{name: "malformed share subject", proof: signDocumentShareUnlockTestJWS(exactHeader, fmt.Sprintf(`{"sub":%q,"iat":%d,"exp":%d}`, strings.ToUpper(shareID), now.Unix(), now.Unix()+900), key)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// When
			err := ValidateDocumentShareUnlockProof(test.proof, shareID, now)

			// Then
			if !errors.Is(err, ErrInvalidDocumentShareUnlockProof) {
				t.Fatalf("ValidateDocumentShareUnlockProof() error = %v, want generic proof error", err)
			}
		})
	}
}
