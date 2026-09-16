package config

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestStructuredDatabaseConfiguration(t *testing.T) {
	previous := v
	t.Cleanup(func() { v = previous })
	for _, host := range []string{"postgres", "::1", "[::1]"} {
		t.Run(host, func(t *testing.T) {
			v = viper.New()
			setDefaults()
			v.Set("database.enabled", true)
			v.Set("database.host", host)
			v.Set("database.user", "user@example")
			v.Set("database.password", "test:p@ss/word#?% with space")
			v.Set("database.name", "vdoc test")
			cfg, err := readConfig()
			if err != nil {
				t.Fatal(err)
			}
			connection, err := url.Parse(cfg.DatabaseDSN)
			if err != nil {
				t.Fatal(err)
			}
			password, _ := connection.User.Password()
			if connection.Hostname() != strings.Trim(host, "[]") || connection.Port() != "5432" || connection.User.Username() != "user@example" || password != "test:p@ss/word#?% with space" || connection.Path != "/vdoc test" || connection.Query().Get("sslmode") != "disable" {
				t.Fatal("structured database settings did not survive URL encoding")
			}
			v.Set("database.dsn", "postgres://legacy:configured@legacy:5432/legacy?sslmode=require")
			cfg, err = readConfig()
			if err != nil || cfg.DatabaseDSN != v.GetString("database.dsn") {
				t.Fatal("explicit legacy DSN must take precedence")
			}
		})
	}
}

func TestStructuredDatabaseRejectsInvalidConfiguration(t *testing.T) {
	previous := v
	t.Cleanup(func() { v = previous })
	for _, test := range []struct {
		key   string
		value any
	}{
		{"database.password", ""}, {"database.password", "CHANGE_ME_DATABASE_PASSWORD"},
		{"database.port", 0}, {"database.port", 65536}, {"database.ssl_mode", "unknown"},
	} {
		t.Run(test.key+"/"+strings.ReplaceAll(strings.TrimSpace(fmt.Sprint(test.value)), " ", "_"), func(t *testing.T) {
			v = viper.New()
			setDefaults()
			v.Set("database.enabled", true)
			v.Set("database.host", "postgres")
			v.Set("database.password", "unit-test-password")
			v.Set(test.key, test.value)
			if _, err := readConfig(); err == nil {
				t.Fatal("invalid database configuration accepted")
			}
		})
	}
}

func TestDeploymentPlaceholdersAreRejectedWithoutLeakingValues(t *testing.T) {
	secret := "CHANGE_ME_sensitive-suffix-that-must-not-be-logged"
	for _, check := range []func() error{
		func() error { return validateJWTConfig(secret, 1) },
		func() error {
			return validateMCPTokenCipherConfig(loadedConfig{MCPTokenCipherKey: secret, MCPTokenCipherKID: "local-aes-gcm-v1"})
		},
		func() error { return ValidateInitialAdminPassword(secret) },
		func() error {
			return validateStorageConfig(loadedConfig{StorageEnabled: true, StorageEndpoint: "rustfs:9000", StorageBucket: "vdoc", StorageAccessKey: "vdoc-test", StorageSecretKey: secret})
		},
	} {
		err := check()
		if err == nil {
			t.Fatal("deployment placeholder accepted")
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatal("configuration error leaked the configured secret")
		}
	}
}
