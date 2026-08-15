package filestorage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/lifecycle"
)

type Config struct {
	Endpoint  string `env:"S3_ENDPOINT" envDefault:"localhost:9000"`
	AccessKey string `env:"S3_ACCESS_KEY"`
	SecretKey string `env:"S3_SECRET_KEY"`
	Bucket    string `env:"S3_BUCKET"`
	Region    string `env:"S3_REGION" envDefault:"us-east-1"`
	UseSSL    bool   `env:"S3_USE_SSL"`

	PublicEndpoint string `env:"S3_PUBLIC_ENDPOINT"`

	PresignTTL time.Duration `env:"S3_PRESIGN_TTL" envDefault:"15m"`

	CreateBucket bool `env:"S3_CREATE_BUCKET" envDefault:"true"`

	StagingPrefix string `env:"S3_STAGING_PREFIX" envDefault:"staging/"`
	StagingTTL    int    `env:"S3_STAGING_TTL_DAYS" envDefault:"1"`
}

type Client struct {
	api    *minio.Client
	public *minio.Client
	cfg    Config
}

type Info struct {
	Key         string
	Size        int64
	ContentType string
	ModifiedAt  time.Time
}

var ErrNotFound = errors.New("filestorage: object not found")

func Open(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("filestorage: S3_BUCKET is required")
	}
	if cfg.PresignTTL <= 0 {
		cfg.PresignTTL = 15 * time.Minute
	}

	api, err := newAPI(cfg, cfg.Endpoint)
	if err != nil {
		return nil, err
	}

	client := &Client{api: api, public: api, cfg: cfg}

	if cfg.PublicEndpoint != "" && cfg.PublicEndpoint != cfg.Endpoint {
		public, err := newAPI(cfg, cfg.PublicEndpoint)
		if err != nil {
			return nil, err
		}
		client.public = public
	}

	if cfg.CreateBucket {
		exists, err := api.BucketExists(ctx, cfg.Bucket)
		if err != nil {
			return nil, fmt.Errorf("filestorage: check bucket %s: %w", cfg.Bucket, err)
		}
		if !exists {
			if err := api.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{Region: cfg.Region}); err != nil {
				return nil, fmt.Errorf("filestorage: create bucket %s: %w", cfg.Bucket, err)
			}
		}
	}

	if err := client.applyLifecycle(ctx); err != nil {
		return nil, err
	}

	return client, nil
}

func (c *Client) applyLifecycle(ctx context.Context) error {
	if c.cfg.StagingPrefix == "" || c.cfg.StagingTTL <= 0 {
		return nil
	}

	config := lifecycle.NewConfiguration()
	config.Rules = []lifecycle.Rule{{
		ID:         "expire-staging",
		Status:     "Enabled",
		RuleFilter: lifecycle.Filter{Prefix: c.cfg.StagingPrefix},
		Expiration: lifecycle.Expiration{Days: lifecycle.ExpirationDays(c.cfg.StagingTTL)},
	}}

	if err := c.api.SetBucketLifecycle(ctx, c.cfg.Bucket, config); err != nil {
		return fmt.Errorf("filestorage: set lifecycle on %s: %w", c.cfg.Bucket, err)
	}

	return nil
}

func newAPI(cfg Config, endpoint string) (*minio.Client, error) {
	api, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("filestorage: connect %s: %w", endpoint, err)
	}

	return api, nil
}

func (c *Client) Ping(ctx context.Context) error {
	if _, err := c.api.BucketExists(ctx, c.cfg.Bucket); err != nil {
		return fmt.Errorf("filestorage: ping: %w", err)
	}

	return nil
}

func (c *Client) PresignPut(ctx context.Context, key string, ttl time.Duration) (string, error) {
	link, err := c.public.PresignedPutObject(ctx, c.cfg.Bucket, key, c.ttlOr(ttl))
	if err != nil {
		return "", fmt.Errorf("filestorage: presign put %s: %w", key, err)
	}

	return link.String(), nil
}

func (c *Client) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if key == "" {
		return "", nil
	}

	link, err := c.public.PresignedGetObject(ctx, c.cfg.Bucket, key, c.ttlOr(ttl), url.Values{})
	if err != nil {
		return "", fmt.Errorf("filestorage: presign get %s: %w", key, err)
	}

	return link.String(), nil
}

func (c *Client) Stat(ctx context.Context, key string) (Info, error) {
	info, err := c.api.StatObject(ctx, c.cfg.Bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).StatusCode == 404 {
			return Info{}, ErrNotFound
		}
		return Info{}, fmt.Errorf("filestorage: stat %s: %w", key, err)
	}

	return Info{
		Key:         info.Key,
		Size:        info.Size,
		ContentType: info.ContentType,
		ModifiedAt:  info.LastModified,
	}, nil
}

func (c *Client) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	object, err := c.api.GetObject(ctx, c.cfg.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("filestorage: get %s: %w", key, err)
	}

	return object, nil
}

func (c *Client) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := c.api.PutObject(ctx, c.cfg.Bucket, key, r, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("filestorage: put %s: %w", key, err)
	}

	return nil
}

func (c *Client) Remove(ctx context.Context, key string) error {
	if key == "" {
		return nil
	}

	if err := c.api.RemoveObject(ctx, c.cfg.Bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("filestorage: remove %s: %w", key, err)
	}

	return nil
}

func (c *Client) ttlOr(ttl time.Duration) time.Duration {
	if ttl > 0 {
		return ttl
	}

	return c.cfg.PresignTTL
}
