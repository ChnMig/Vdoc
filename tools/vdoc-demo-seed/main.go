package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	defaultBaseURL  = "http://127.0.0.1:8080"
	defaultPassword = "local-demo-password-not-printed-32chars"
	defaultTimeout  = 20 * time.Second
	roleWriter      = 2
	docTypeOpenAPI  = 1
	scopeAPIRead    = 1
	scopeAPIDraft   = 2
)

type config struct {
	BaseURL  string
	Email    string
	Password string
	RunID    string
	Timeout  time.Duration
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cfg, err := parseConfig(os.Args[1:], os.Getenv)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := runSeed(ctx, cfg, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseConfig(args []string, getenv func(string) string) (config, error) {
	cfg := config{BaseURL: first(getenv("VDOC_BASE_URL"), defaultBaseURL), Email: getenv("VDOC_EMAIL"), Password: getenv("VDOC_PASSWORD"), Timeout: defaultTimeout, RunID: getenv("VDOC_DEMO_RUN_ID")}
	fs := flag.NewFlagSet("vdoc-demo-seed", flag.ContinueOnError)
	fs.StringVar(&cfg.BaseURL, "base-url", cfg.BaseURL, "local Vdoc backend URL (env: VDOC_BASE_URL)")
	fs.StringVar(&cfg.Email, "email", cfg.Email, "existing admin email; omit to register a disposable admin (env: VDOC_EMAIL)")
	fs.StringVar(&cfg.Password, "password", cfg.Password, "existing admin password; omit with --email only is invalid (env: VDOC_PASSWORD)")
	fs.StringVar(&cfg.RunID, "run-id", cfg.RunID, "unique suffix for demo resources (env: VDOC_DEMO_RUN_ID)")
	fs.DurationVar(&cfg.Timeout, "timeout", cfg.Timeout, "HTTP request timeout")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if cfg.BaseURL == "" {
		return config{}, fmt.Errorf("base URL is required")
	}
	if cfg.RunID == "" {
		cfg.RunID = "demo-" + randomHex(4)
	}
	if cfg.Email != "" && cfg.Password == "" {
		return config{}, fmt.Errorf("--password is required when --email is provided")
	}
	return cfg, nil
}

func runSeed(ctx context.Context, cfg config, out io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	client := apiClient{baseURL: cfg.BaseURL, http: &http.Client{Timeout: cfg.Timeout}}
	result, err := seedWorkspace(ctx, client, cfg)
	if err != nil {
		return err
	}
	renderResult(out, result, cfg.BaseURL)
	return nil
}

func first(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func randomHex(bytesLen int) string {
	b := make([]byte, bytesLen)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
