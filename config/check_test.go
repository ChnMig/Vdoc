package config

import "testing"

func TestValidateConfig(t *testing.T) {
	valid := func() loadedConfig {
		return loadedConfig{
			JWTKey:              "0123456789abcdef0123456789abcdef",
			JWTExpiration:       12,
			DatabaseMaxOpenConn: 20,
			DatabaseMaxIdleConn: 5,
			StorageBucket:       "vdoc",
			MCPTokenCipherKID:   "local-aes-gcm-v1",
		}
	}
	tests := []struct {
		name    string
		mutate  func(*loadedConfig)
		wantErr bool
	}{
		{
			name: "valid config",
		},
		{
			name:    "empty key",
			mutate:  func(cfg *loadedConfig) { cfg.JWTKey = "" },
			wantErr: true,
		},
		{
			name:    "template key",
			mutate:  func(cfg *loadedConfig) { cfg.JWTKey = "YOUR_SECRET_KEY_HERE_AT_LEAST_32_CHARACTERS" },
			wantErr: true,
		},
		{
			name:    "production example key",
			mutate:  func(cfg *loadedConfig) { cfg.JWTKey = "production_secret_key_min_32_chars" },
			wantErr: true,
		},
		{
			name:    "short key",
			mutate:  func(cfg *loadedConfig) { cfg.JWTKey = "short" },
			wantErr: true,
		},
		{
			name:    "missing expiration",
			mutate:  func(cfg *loadedConfig) { cfg.JWTExpiration = 0 },
			wantErr: true,
		},
		{
			name: "database enabled with dsn",
			mutate: func(cfg *loadedConfig) {
				cfg.DatabaseEnabled = true
				cfg.DatabaseDSN = "postgres://vdoc@127.0.0.1:5432/vdoc?sslmode=disable"
			},
		},
		{
			name: "database enabled without dsn",
			mutate: func(cfg *loadedConfig) {
				cfg.DatabaseEnabled = true
				cfg.DatabaseDSN = ""
			},
			wantErr: true,
		},
		{
			name: "database idle exceeds open",
			mutate: func(cfg *loadedConfig) {
				cfg.DatabaseEnabled = true
				cfg.DatabaseDSN = "postgres://vdoc@127.0.0.1:5432/vdoc?sslmode=disable"
				cfg.DatabaseMaxOpenConn = 1
				cfg.DatabaseMaxIdleConn = 2
			},
			wantErr: true,
		},
		{
			name: "storage enabled with required settings",
			mutate: func(cfg *loadedConfig) {
				cfg.StorageEnabled = true
				cfg.StorageEndpoint = "127.0.0.1:9000"
				cfg.StorageBucket = "vdoc"
				cfg.StorageAccessKey = "test-access"
				cfg.StorageSecretKey = "test-secret"
			},
		},
		{
			name: "storage enabled without credentials",
			mutate: func(cfg *loadedConfig) {
				cfg.StorageEnabled = true
				cfg.StorageEndpoint = "127.0.0.1:9000"
				cfg.StorageBucket = "vdoc"
			},
			wantErr: true,
		},
		{
			name: "storage endpoint with scheme",
			mutate: func(cfg *loadedConfig) {
				cfg.StorageEnabled = true
				cfg.StorageEndpoint = "http://127.0.0.1:9000"
				cfg.StorageBucket = "vdoc"
				cfg.StorageAccessKey = "test-access"
				cfg.StorageSecretKey = "test-secret"
			},
			wantErr: true,
		},
		{
			name: "initial admin configured",
			mutate: func(cfg *loadedConfig) {
				cfg.InitialAdminEmail = "admin@example.com"
				cfg.InitialAdminName = "Root Admin"
				cfg.InitialAdminPassword = "Password123456"
			},
		},
		{
			name: "initial admin disabled compose defaults use blank triplet",
			mutate: func(cfg *loadedConfig) {
				cfg.InitialAdminEmail = ""
				cfg.InitialAdminName = ""
				cfg.InitialAdminPassword = ""
			},
		},
		{
			name: "initial admin disabled rejects name only default",
			mutate: func(cfg *loadedConfig) {
				cfg.InitialAdminName = "VdocAdmin"
			},
			wantErr: true,
		},
		{
			name: "initial admin missing email",
			mutate: func(cfg *loadedConfig) {
				cfg.InitialAdminPassword = "Password123456"
			},
			wantErr: true,
		},
		{
			name: "initial admin missing password",
			mutate: func(cfg *loadedConfig) {
				cfg.InitialAdminEmail = "admin@example.com"
			},
			wantErr: true,
		},
		{
			name: "initial admin short password",
			mutate: func(cfg *loadedConfig) {
				cfg.InitialAdminEmail = "admin@example.com"
				cfg.InitialAdminPassword = "short"
			},
			wantErr: true,
		},
		{
			name: "unsupported mcp cipher kid",
			mutate: func(cfg *loadedConfig) {
				cfg.MCPTokenCipherKID = "local-aes-gcm-v0"
			},
			wantErr: true,
		},
		{
			name: "short explicit mcp cipher key",
			mutate: func(cfg *loadedConfig) {
				cfg.MCPTokenCipherKey = "short"
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid()
			if tt.mutate != nil {
				tt.mutate(&cfg)
			}
			err := validateConfig(cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
