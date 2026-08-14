package documentshare

import (
	"errors"
	"reflect"
	"testing"
	"time"

	commonvdoc "vdoc/common/vdoc"
)

func TestDocumentShareEffectiveStatus_honorsExpiryBoundaryAndPrecedence(t *testing.T) {
	// Given
	expiresAt := time.Date(2026, time.October, 20, 8, 0, 0, 0, time.UTC)
	active := validShare(expiresAt.AddDate(0, -3, 0))
	active.ExpiresAt = &expiresAt
	revoked := Clone(active)
	revoked.Status = commonvdoc.DocumentShareStatusRevoked
	revokedBy := "admin-b"
	revokedAt := expiresAt.Add(-time.Hour)
	revoked.RevokedBy = &revokedBy
	revoked.RevokedAt = &revokedAt
	tests := []struct {
		name  string
		share *DocumentShare
		now   time.Time
		want  int
	}{
		{name: "active one nanosecond before expiry", share: active, now: expiresAt.Add(-time.Nanosecond), want: commonvdoc.DocumentShareStatusActive},
		{name: "expired at equality", share: active, now: expiresAt, want: commonvdoc.DocumentShareStatusExpired},
		{name: "revoked before expired", share: revoked, now: expiresAt, want: commonvdoc.DocumentShareStatusRevoked},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// When
			got, err := EffectiveStatus(test.share, test.now)

			// Then
			if err != nil {
				t.Fatalf("EffectiveStatus() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("EffectiveStatus() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestDocumentShareEffectiveStatus_rejectsInvalidPersistedCodes(t *testing.T) {
	// Given
	now := time.Date(2026, time.July, 20, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		scope  int
		status int
	}{
		{name: "zero scope", scope: 0, status: commonvdoc.DocumentShareStatusActive},
		{name: "unknown scope", scope: 3, status: commonvdoc.DocumentShareStatusActive},
		{name: "zero status", scope: commonvdoc.DocumentShareScopeLatest, status: 0},
		{name: "computed status persisted", scope: commonvdoc.DocumentShareScopeLatest, status: commonvdoc.DocumentShareStatusExpired},
		{name: "unknown status", scope: commonvdoc.DocumentShareScopeLatest, status: 4},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			share := validShare(now)
			share.VersionScope = test.scope
			share.Status = test.status

			// When
			_, err := EffectiveStatus(share, now)

			// Then
			if !errors.Is(err, commonvdoc.ErrInvalidArgument) {
				t.Fatalf("EffectiveStatus() error = %v, want invalid argument", err)
			}
		})
	}
}

func TestDocumentShareReveal_requiresEffectiveActiveState(t *testing.T) {
	// Given
	expiresAt := time.Date(2026, time.October, 20, 8, 0, 0, 0, time.UTC)
	active := validShare(expiresAt.AddDate(0, -3, 0))
	active.ExpiresAt = &expiresAt
	revoked := Clone(active)
	revoked.Status = commonvdoc.DocumentShareStatusRevoked
	revokedBy := "admin-b"
	revokedAt := expiresAt.Add(-time.Hour)
	revoked.RevokedBy = &revokedBy
	revoked.RevokedAt = &revokedAt
	tests := []struct {
		name    string
		share   *DocumentShare
		now     time.Time
		wantErr bool
	}{
		{name: "active", share: active, now: expiresAt.Add(-time.Nanosecond)},
		{name: "expired", share: active, now: expiresAt, wantErr: true},
		{name: "revoked", share: revoked, now: expiresAt.Add(-time.Nanosecond), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// When
			err := EnsureRevealable(test.share, test.now)

			// Then
			if test.wantErr && !errors.Is(err, commonvdoc.ErrFailedPrecondition) {
				t.Fatalf("EnsureRevealable() error = %v, want failed precondition", err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("EnsureRevealable() error = %v", err)
			}
		})
	}
}

func TestDocumentShareRevoke_returnsImmutableOneWayTransition(t *testing.T) {
	// Given
	createdAt := time.Date(2026, time.July, 20, 8, 0, 0, 0, time.UTC)
	original := validShare(createdAt)
	revokedAt := createdAt.Add(time.Hour)

	// When
	revoked, transitioned, err := Revoke(original, "admin-b", revokedAt)

	// Then
	if err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if !transitioned {
		t.Fatal("Revoke() transitioned = false, want true")
	}
	if original.Status != commonvdoc.DocumentShareStatusActive || original.RevokedAt != nil || original.RevokedBy != nil {
		t.Fatalf("original share mutated = %+v", original)
	}
	if revoked == original || revoked.Status != commonvdoc.DocumentShareStatusRevoked {
		t.Fatalf("revoked share = %+v", revoked)
	}
	if revoked.RevokedBy == nil || *revoked.RevokedBy != "admin-b" || revoked.RevokedAt == nil || !revoked.RevokedAt.Equal(revokedAt) {
		t.Fatalf("revocation metadata = by %v at %v", revoked.RevokedBy, revoked.RevokedAt)
	}
}

func TestDocumentShareRevoke_returnsUnchangedStateWhenRepeated(t *testing.T) {
	// Given
	createdAt := time.Date(2026, time.July, 20, 8, 0, 0, 0, time.UTC)
	first, transitioned, err := Revoke(validShare(createdAt), "admin-b", createdAt.Add(time.Hour))
	if err != nil || !transitioned {
		t.Fatalf("first Revoke() = transitioned %v, error %v", transitioned, err)
	}

	// When
	repeated, transitioned, err := Revoke(first, "admin-c", createdAt.Add(2*time.Hour))

	// Then
	if err != nil {
		t.Fatalf("repeated Revoke() error = %v", err)
	}
	if transitioned {
		t.Fatal("repeated Revoke() transitioned = true, want false")
	}
	if repeated == first || !reflect.DeepEqual(repeated, first) {
		t.Fatalf("repeated Revoke() = %+v, want unchanged clone %+v", repeated, first)
	}
}

func TestDocumentShareRevoke_rejectsExpiredState(t *testing.T) {
	// Given
	createdAt := time.Date(2026, time.July, 20, 8, 0, 0, 0, time.UTC)
	share := validShare(createdAt)
	expiresAt := createdAt.Add(time.Hour)
	share.ExpiresAt = &expiresAt

	// When
	_, transitioned, err := Revoke(share, "admin-b", expiresAt)

	// Then
	if !errors.Is(err, commonvdoc.ErrFailedPrecondition) {
		t.Fatalf("Revoke() error = %v, want failed precondition", err)
	}
	if transitioned {
		t.Fatal("Revoke() transitioned = true, want false")
	}
	if share.Status != commonvdoc.DocumentShareStatusActive || share.RevokedAt != nil || share.RevokedBy != nil {
		t.Fatalf("expired share mutated = %+v", share)
	}
}

func validCreateParams(now time.Time, preset ExpiryPreset) CreateParams {
	verifier := "encoded"
	return CreateParams{
		ID:               "share-a",
		ProjectID:        "project-a",
		DocumentID:       "document-a",
		BranchID:         "branch-a",
		TokenHash:        "hash",
		TokenCiphertext:  []byte("ciphertext"),
		CipherKID:        "document-share-aes-gcm-v1",
		PasswordVerifier: &verifier,
		VersionScope:     commonvdoc.DocumentShareScopeLatest,
		ExpiryPreset:     preset,
		CreatedBy:        "admin-a",
		Now:              now,
	}
}

func createParamsWithScope(now time.Time, scope int) CreateParams {
	params := validCreateParams(now, ExpiryPresetThreeMonths)
	params.VersionScope = scope
	return params
}

func validShare(createdAt time.Time) *DocumentShare {
	share, err := Create(validCreateParams(createdAt, ExpiryPresetThreeMonths))
	if err != nil {
		panic(err)
	}
	return share
}
