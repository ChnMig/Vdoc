package config

import "testing"

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name          string
		jwtKey        string
		jwtExpiration int64
		wantErr       bool
	}{
		{
			name:          "valid config",
			jwtKey:        "0123456789abcdef0123456789abcdef",
			jwtExpiration: int64(12),
		},
		{
			name:          "empty key",
			jwtKey:        "",
			jwtExpiration: int64(12),
			wantErr:       true,
		},
		{
			name:          "template key",
			jwtKey:        "YOUR_SECRET_KEY_HERE_AT_LEAST_32_CHARACTERS",
			jwtExpiration: int64(12),
			wantErr:       true,
		},
		{
			name:          "production example key",
			jwtKey:        "production_secret_key_min_32_chars",
			jwtExpiration: int64(12),
			wantErr:       true,
		},
		{
			name:          "short key",
			jwtKey:        "short",
			jwtExpiration: int64(12),
			wantErr:       true,
		},
		{
			name:    "missing expiration",
			jwtKey:  "0123456789abcdef0123456789abcdef",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.jwtKey, tt.jwtExpiration)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
