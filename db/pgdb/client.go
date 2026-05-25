package pgdb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"vdoc/config"
	vdocdb "vdoc/db"

	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Config struct {
	DSN          string
	MaxOpenConn  int
	MaxIdleConn  int
	RunMigration bool
}

type Client struct {
	database *gorm.DB
	pool     *sql.DB
}

func Open(ctx context.Context) (*Client, error) {
	return OpenWithConfig(ctx, Config{
		DSN:          config.DatabaseDSN,
		MaxOpenConn:  config.DatabaseMaxOpenConn,
		MaxIdleConn:  config.DatabaseMaxIdleConn,
		RunMigration: true,
	})
}

func OpenWithConfig(ctx context.Context, cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, fmt.Errorf("database.dsn is required when database.enabled=true")
	}
	gormLogger := logger.New(zap.NewStdLog(zap.L()), logger.Config{
		SlowThreshold:             time.Second,
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: true,
		Colorful:                  false,
	})
	database, err := gorm.Open(postgres.Open(cfg.DSN), &gorm.Config{Logger: gormLogger})
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	pool, err := database.DB()
	if err != nil {
		return nil, fmt.Errorf("postgres pool: %w", err)
	}
	pool.SetMaxOpenConns(cfg.MaxOpenConn)
	pool.SetMaxIdleConns(cfg.MaxIdleConn)
	client := &Client{database: database, pool: pool}
	if err := client.Ping(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	if cfg.RunMigration {
		if err := vdocdb.RunMigrations(ctx, database); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("run postgres migrations: %w", err)
		}
	}
	return client, nil
}

func (c *Client) DB() *gorm.DB {
	if c == nil {
		return nil
	}
	return c.database
}

func (c *Client) Ping(ctx context.Context) error {
	if c == nil || c.pool == nil {
		return fmt.Errorf("postgres client is not initialized")
	}
	if err := c.pool.PingContext(ctx); err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	return nil
}

func (c *Client) Close() error {
	if c == nil || c.pool == nil {
		return nil
	}
	return c.pool.Close()
}
