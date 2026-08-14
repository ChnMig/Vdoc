package documentshare

import (
	"errors"
	"testing"
	"time"

	commonvdoc "vdoc/common/vdoc"
)

func TestDocumentShareCreate_appliesExpiryPresetsInUTC(t *testing.T) {
	// Given
	location := time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2024, time.January, 31, 23, 45, 0, 0, location)
	tests := []struct {
		name   string
		preset ExpiryPreset
		years  int
		months int
	}{
		{name: "one month", preset: ExpiryPresetOneMonth, months: 1},
		{name: "three months", preset: ExpiryPresetThreeMonths, months: 3},
		{name: "six months", preset: ExpiryPresetSixMonths, months: 6},
		{name: "one year", preset: ExpiryPresetOneYear, years: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// When
			share, err := Create(validCreateParams(now, test.preset))

			// Then
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			want := now.UTC().AddDate(test.years, test.months, 0)
			if share.ExpiresAt == nil || !share.ExpiresAt.Equal(want) {
				t.Fatalf("ExpiresAt = %v, want %v", share.ExpiresAt, want)
			}
			if share.ExpiresAt.Location() != time.UTC {
				t.Fatalf("ExpiresAt location = %v, want UTC", share.ExpiresAt.Location())
			}
		})
	}
}

func TestDocumentShareCreate_keepsPermanentExpiryAbsent(t *testing.T) {
	// Given
	params := validCreateParams(time.Date(2026, time.July, 20, 8, 0, 0, 0, time.UTC), ExpiryPresetPermanent)

	// When
	share, err := Create(params)

	// Then
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if share.ExpiresAt != nil {
		t.Fatalf("ExpiresAt = %v, want nil", share.ExpiresAt)
	}
}

func TestDocumentShareCreate_rejectsUnknownPresetAndScope(t *testing.T) {
	// Given
	now := time.Date(2026, time.July, 20, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		params CreateParams
	}{
		{name: "unknown preset", params: validCreateParams(now, ExpiryPreset("forever"))},
		{name: "zero scope", params: createParamsWithScope(now, 0)},
		{name: "unknown scope", params: createParamsWithScope(now, 3)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// When
			_, err := Create(test.params)

			// Then
			if !errors.Is(err, commonvdoc.ErrInvalidArgument) {
				t.Fatalf("Create() error = %v, want invalid argument", err)
			}
		})
	}
}

func TestDocumentShareCreate_derivesProtectionFromNullableVerifier(t *testing.T) {
	// Given
	now := time.Date(2026, time.July, 20, 8, 0, 0, 0, time.UTC)
	protectedParams := validCreateParams(now, ExpiryPresetThreeMonths)
	unprotectedParams := validCreateParams(now, ExpiryPresetThreeMonths)
	unprotectedParams.PasswordVerifier = nil

	// When
	protectedShare, protectedErr := Create(protectedParams)
	unprotectedShare, unprotectedErr := Create(unprotectedParams)

	// Then
	if protectedErr != nil || unprotectedErr != nil {
		t.Fatalf("Create() errors = protected %v, unprotected %v", protectedErr, unprotectedErr)
	}
	if !protectedShare.PasswordProtected() {
		t.Fatal("PasswordProtected() = false, want true")
	}
	if unprotectedShare.PasswordProtected() {
		t.Fatal("PasswordProtected() = true, want false")
	}
}
