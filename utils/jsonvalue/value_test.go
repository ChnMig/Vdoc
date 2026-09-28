package jsonvalue

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExactNumbersAndEquivalentSpellings(t *testing.T) {
	for _, pair := range [][2]string{{"9007199254740993", "9007199254740993"}, {"1.0", "1"}, {"1e0", "1"}, {"-0.000", "0"}, {"1.234e2", "123.4"}, {"0.00000000001", "1e-11"}, {"1e131071", "1e131071"}, {"1e-16383", "1e-16383"}, {"10e-16384", "1e-16383"}, {"0e200000", "0"}} {
		got, err := Number(pair[0])
		if err != nil || string(got) != pair[1] {
			t.Fatalf("%s: %s %v", pair[0], got, err)
		}
	}
	var value any
	if err := Decode([]byte(`{"enum":[9007199254740993]}`), &value); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(value)
	if string(encoded) != `{"enum":[9007199254740993]}` {
		t.Fatal(string(encoded))
	}
	if Decode([]byte(`1 2`), &value) == nil {
		t.Fatal("multiple JSON documents accepted")
	}
}

func TestNumberRejectsOutOfStorageRange(t *testing.T) {
	for _, raw := range []string{"1e131072", "-1e131072", "1e-16384", "1.1e-16383", "1e200000"} {
		if _, err := Number(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestNormalizeExpandedStoredNumbers(t *testing.T) {
	for _, tc := range [][2]string{{"1" + strings.Repeat("0", 131071), "1e131071"}, {"0." + strings.Repeat("0", 16382) + "1", "1e-16383"}} {
		if got := Normalize(json.Number(tc[0])); got != json.Number(tc[1]) {
			t.Fatal("JSONB expansion changed numeric identity")
		}
	}
}
