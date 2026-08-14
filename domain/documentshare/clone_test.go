package documentshare

import (
	"reflect"
	"testing"
	"time"
)

func TestDocumentShareClone_deepCopiesMutableFields(t *testing.T) {
	// Given
	createdAt := time.Date(2026, time.July, 20, 8, 0, 0, 0, time.UTC)
	original, transitioned, err := Revoke(validShare(createdAt), "admin-b", createdAt.Add(time.Hour))
	if err != nil || !transitioned {
		t.Fatalf("Revoke() = transitioned %v, error %v", transitioned, err)
	}

	// When
	cloned := Clone(original)
	cloned.TokenCiphertext[0] = 'X'
	*cloned.PasswordVerifier = "changed"
	*cloned.RevokedBy = "changed"
	changedExpiry := cloned.ExpiresAt.Add(time.Hour)
	cloned.ExpiresAt = &changedExpiry
	changedRevocation := cloned.RevokedAt.Add(time.Hour)
	cloned.RevokedAt = &changedRevocation

	// Then
	if reflect.DeepEqual(cloned, original) {
		t.Fatal("Clone() shares mutable fields with original")
	}
	if string(original.TokenCiphertext) != "ciphertext" || *original.PasswordVerifier != "encoded" || *original.RevokedBy != "admin-b" {
		t.Fatalf("original mutable fields changed = %+v", original)
	}
	if !original.ExpiresAt.Equal(createdAt.AddDate(0, 3, 0)) || !original.RevokedAt.Equal(createdAt.Add(time.Hour)) {
		t.Fatalf("original timestamps changed = expires %v revoked %v", original.ExpiresAt, original.RevokedAt)
	}
}
