package documentversion

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestContractVersionJSONOmitsRelativePath(t *testing.T) {
	// Given
	version := ContractVersion{ID: "version-a", RelativePath: "apis/private-checkout.yaml"}

	// When
	payload, err := json.Marshal(version)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	// Then
	if strings.Contains(string(payload), "relative_path") || strings.Contains(string(payload), version.RelativePath) {
		t.Fatalf("ContractVersion JSON exposed immutable relative path: %s", payload)
	}
}
