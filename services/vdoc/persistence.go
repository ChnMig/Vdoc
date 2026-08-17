package vdoc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"strings"

	"vdoc/config"
	domainvdoc "vdoc/domain/vdoc"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type RuntimeConfig struct {
	DatabaseEnabled        bool
	DatabaseDSN            string
	DatabaseMaxOpenConn    int
	DatabaseMaxIdleConn    int
	DatabaseRepository     domainvdoc.Repository
	DatabaseClose          func() error
	StorageEnabled         bool
	StorageEndpoint        string
	StorageBucket          string
	StorageAccessKey       string
	StorageSecretKey       string
	StorageRegion          string
	StorageUseSSL          bool
	StoragePathStyle       bool
	ObjectStorage          ObjectStorage
	InitialAdminEmail      string
	InitialAdminName       string
	InitialAdminPassword   string
	AllowRegistration      bool
	RequireBootstrapAccess bool
}

type postgresPersistence struct {
	repo     domainvdoc.Repository
	close    func() error
	revision string
}

type ObjectWrite struct {
	Key         string
	ContentType string
	Body        []byte
	Metadata    map[string]string
}

type ObjectInfo struct {
	ETag      string
	SizeBytes int64
	Metadata  map[string]string
}

type ObjectStorage interface {
	PutObject(ctx context.Context, write ObjectWrite) (ObjectInfo, error)
	GetObject(ctx context.Context, key string) ([]byte, error)
	DeleteObject(ctx context.Context, key string) error
	HealthCheck(ctx context.Context) error
}

const defaultMaxStoredObjectBytes int64 = 10 << 20

func maxStoredObjectBytes() int64 {
	if config.MaxBodySize > 0 {
		return config.MaxBodySize
	}
	return defaultMaxStoredObjectBytes
}

func readStoredObjectBody(reader io.Reader) ([]byte, error) {
	limit := maxStoredObjectBytes()
	if limit >= math.MaxInt64 {
		return nil, fmt.Errorf("stored object read limit is too large")
	}
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("stored object exceeds %d byte limit", limit)
	}
	return body, nil
}

type objectStorage struct {
	client *minio.Client
	bucket string
}

func InitDefaultStore(ctx context.Context, cfg RuntimeConfig) error {
	store := NewStore()
	if cfg.ObjectStorage != nil {
		store.objects = cfg.ObjectStorage
	} else if cfg.StorageEnabled {
		storage, err := newObjectStorage(ctx, cfg)
		if err != nil {
			return fmt.Errorf("initialize object storage: %w", err)
		}
		store.objects = storage
	}
	if cfg.DatabaseEnabled {
		if cfg.DatabaseRepository == nil {
			return fmt.Errorf("database repository is required when database.enabled=true")
		}
		p := &postgresPersistence{repo: cfg.DatabaseRepository, close: cfg.DatabaseClose}
		store.persistence = p
		if _, err := p.load(ctx, store); err != nil {
			if p.close != nil {
				_ = p.close()
			}
			return fmt.Errorf("load database-backed Vdoc state: %w", err)
		}
		store.persisted = store.cloneStateLocked()
	}
	if err := store.SeedInitialAdmin(cfg.InitialAdminEmail, cfg.InitialAdminName, cfg.InitialAdminPassword); err != nil {
		if cfg.DatabaseClose != nil {
			_ = cfg.DatabaseClose()
		}
		return fmt.Errorf("seed initial admin: %w", err)
	}
	if cfg.RequireBootstrapAccess && !hasBootstrapAccess(store.users, cfg.AllowRegistration) {
		if cfg.DatabaseClose != nil {
			_ = cfg.DatabaseClose()
		}
		return fmt.Errorf("bootstrap access unavailable: configure an active initial_admin, or enable registration only for an empty trusted pilot deployment")
	}
	defaultStore = store
	return nil
}

func hasBootstrapAccess(users map[string]*User, allowRegistration bool) bool {
	for _, user := range users {
		if user != nil && user.Status == UserStatusActive && user.IsSuperAdmin {
			return true
		}
	}
	return allowRegistration && len(users) == 0
}

func CheckDefaultObjectStorage(ctx context.Context) error {
	if defaultStore == nil || defaultStore.objects == nil {
		return fmt.Errorf("object storage is not initialized")
	}
	return defaultStore.objects.HealthCheck(ctx)
}

func CloseDefaultStore() error {
	if defaultStore == nil || defaultStore.persistence == nil {
		return nil
	}
	if defaultStore.persistence.close == nil {
		return nil
	}
	return defaultStore.persistence.close()
}

func newObjectStorage(ctx context.Context, cfg RuntimeConfig) (*objectStorage, error) {
	if cfg.StorageEndpoint == "" || cfg.StorageBucket == "" || cfg.StorageAccessKey == "" || cfg.StorageSecretKey == "" {
		return nil, fmt.Errorf("storage endpoint, bucket, access_key and secret_key are required when storage.enabled=true")
	}
	options := &minio.Options{Creds: credentials.NewStaticV4(cfg.StorageAccessKey, cfg.StorageSecretKey, ""), Secure: cfg.StorageUseSSL, Region: cfg.StorageRegion}
	if cfg.StoragePathStyle {
		options.BucketLookup = minio.BucketLookupPath
	}
	client, err := minio.New(cfg.StorageEndpoint, options)
	if err != nil {
		return nil, fmt.Errorf("initialize storage client: %w", err)
	}
	exists, err := client.BucketExists(ctx, cfg.StorageBucket)
	if err != nil {
		return nil, fmt.Errorf("check storage bucket: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.StorageBucket, minio.MakeBucketOptions{Region: cfg.StorageRegion}); err != nil {
			return nil, fmt.Errorf("create storage bucket: %w", err)
		}
	}
	return &objectStorage{client: client, bucket: cfg.StorageBucket}, nil
}

func (o *objectStorage) HealthCheck(ctx context.Context) error {
	if o == nil || o.client == nil {
		return fmt.Errorf("object storage client is not initialized")
	}
	exists, err := o.client.BucketExists(ctx, o.bucket)
	if err != nil {
		return fmt.Errorf("check storage bucket: %w", err)
	}
	if !exists {
		return fmt.Errorf("storage bucket %q does not exist", o.bucket)
	}
	return nil
}

func (o *objectStorage) PutObject(ctx context.Context, write ObjectWrite) (ObjectInfo, error) {
	info, err := o.client.PutObject(ctx, o.bucket, write.Key, bytes.NewReader(write.Body), int64(len(write.Body)), minio.PutObjectOptions{ContentType: write.ContentType, UserMetadata: copyStringMap(write.Metadata)})
	if err != nil {
		return ObjectInfo{}, err
	}
	return ObjectInfo{ETag: info.ETag, SizeBytes: info.Size, Metadata: copyStringMap(write.Metadata)}, nil
}

func (o *objectStorage) GetObject(ctx context.Context, key string) ([]byte, error) {
	object, err := o.client.GetObject(ctx, o.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer object.Close()
	return readStoredObjectBody(object)
}

func (o *objectStorage) DeleteObject(ctx context.Context, key string) error {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	return o.client.RemoveObject(ctx, o.bucket, key, minio.RemoveObjectOptions{})
}
