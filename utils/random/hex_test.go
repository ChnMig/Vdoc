package random

import (
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
)

func TestHex_Length(t *testing.T) {
	s, err := Hex(16)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(s) != 32 {
		t.Fatalf("expect len=32, got %d", len(s))
	}
}

func TestHex_Invalid(t *testing.T) {
	if _, err := Hex(0); err == nil {
		t.Fatalf("expect err")
	}
}

func TestBase64URL(t *testing.T) {
	for _, size := range []int{1, 2, 3, 16, 32} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			value, err := Base64URL(size)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := base64.RawURLEncoding.DecodeString(value)
			if err != nil || len(decoded) != size {
				t.Fatalf("invalid Base64URL for %d bytes: length=%d, error=%v", size, len(decoded), err)
			}
			if len(value) != base64.RawURLEncoding.EncodedLen(size) || strings.ContainsAny(value, "+/=") {
				t.Fatalf("encoded value is not an unpadded URL-safe string: %q", value)
			}
		})
	}
	for _, invalid := range []int{0, -1} {
		if _, err := Base64URL(invalid); err == nil {
			t.Fatalf("Base64URL(%d) expected error", invalid)
		}
	}
}
