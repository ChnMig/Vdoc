package documentshare

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	commonvdoc "vdoc/common/vdoc"
)

func TestDocumentShareCredentialInput_acceptsExactByteBoundaries(t *testing.T) {
	// Given
	tests := []struct {
		name  string
		value string
	}{
		{name: "twelve bytes", value: strings.Repeat("a", 12)},
		{name: "seventy two bytes", value: strings.Repeat("b", 72)},
		{name: "interior whitespace", value: "twelve bytes allowed"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// When
			parsed, err := ParsePassword(test.value)

			// Then
			if err != nil {
				t.Fatalf("ParsePassword() error = %v", err)
			}
			if !bytes.Equal(parsed.Bytes(), []byte(test.value)) {
				t.Fatal("ParsePassword() changed input bytes")
			}
		})
	}
}

func TestDocumentShareCredentialInput_rejectsInvalidPresentValues(t *testing.T) {
	// Given
	tests := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "eleven bytes", value: strings.Repeat("a", 11)},
		{name: "seventy three bytes", value: strings.Repeat("b", 73)},
		{name: "invalid utf eight", value: string([]byte{0xff, 0xfe, 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j'})},
		{name: "leading ascii whitespace", value: " twelve-byte-value"},
		{name: "trailing ascii whitespace", value: "twelve-byte-value "},
		{name: "leading unicode whitespace", value: "\u2003twelve-byte-value"},
		{name: "trailing unicode whitespace", value: "twelve-byte-value\u00a0"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// When
			_, err := ParsePassword(test.value)

			// Then
			if !errors.Is(err, commonvdoc.ErrInvalidArgument) {
				t.Fatalf("ParsePassword() error = %v, want invalid argument", err)
			}
		})
	}
}

func TestDocumentShareCredentialInput_preservesUnicodeWithoutNormalization(t *testing.T) {
	// Given
	value := "cafe\u0301-access-value"

	// When
	parsed, err := ParsePassword(value)

	// Then
	if err != nil {
		t.Fatalf("ParsePassword() error = %v", err)
	}
	if !bytes.Equal(parsed.Bytes(), []byte(value)) {
		t.Fatal("ParsePassword() normalized input bytes")
	}
}
