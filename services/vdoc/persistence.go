package vdoc

import (
	"bytes"
	"context"
	"fmt"
	"io"

	domainvdoc "vdoc/domain/vdoc"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type RuntimeConfig struct {
	DatabaseEnabled     bool
	DatabaseDSN         string
	DatabaseMaxOpenConn int
	DatabaseMaxIdleConn int
	DatabaseRepository  domainvdoc.Repository
	DatabaseClose       func() error
	StorageEnabled      bool
	StorageEndpoint     string
	StorageBucket       string
	StorageAccessKey    string
	StorageSecretKey    string
	StorageRegion       string
	StorageUseSSL       bool
	StoragePathStyle    bool
	ObjectStorage       ObjectStorage
}

type postgresPersistence struct {
	repo  domainvdoc.Repository
	close func() error
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
	HealthCheck(ctx context.Context) error
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
		if err := p.load(ctx, store); err != nil {
			if p.close != nil {
				_ = p.close()
			}
			return fmt.Errorf("load database-backed Vdoc state: %w", err)
		}
	}
	defaultStore = store
	return nil
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
	return io.ReadAll(object)
}

func (p *postgresPersistence) recordObject(ctx context.Context, ref domainvdoc.ObjectRef) error {
	return p.repo.RecordObject(ctx, ref)
}
