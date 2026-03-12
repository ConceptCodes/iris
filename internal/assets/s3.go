package assets

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Config struct {
	Bucket       string
	Region       string
	Endpoint     string
	AccessKey    string
	SecretKey    string
	SessionToken string
	Prefix       string
	PublicBase   string
	UsePathStyle bool
}

type S3Store struct {
	client     *s3.Client
	bucket     string
	prefix     string
	publicBase string
	useBaseURL bool
}

type Settings struct {
	Backend string
	S3      S3Config
}

// NewStoreFromSettings creates an S3Store from the provided settings.
// The only supported backend is "s3". An empty backend will return an error,
// enforcing that thumbnails are always stored remotely.
func NewStoreFromSettings(ctx context.Context, settings Settings) (Store, error) {
	switch strings.ToLower(strings.TrimSpace(settings.Backend)) {
	case "s3":
		return NewS3Store(ctx, settings.S3)
	default:
		return nil, fmt.Errorf("unknown asset backend %q: only \"s3\" is supported", settings.Backend)
	}
}

func NewS3Store(ctx context.Context, cfg S3Config) (*S3Store, error) {
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("s3 bucket is required")
	}
	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		if cfg.UsePathStyle {
			o.UsePathStyle = true
		}
	})

	store := &S3Store{
		client:     client,
		bucket:     cfg.Bucket,
		prefix:     strings.Trim(cfg.Prefix, "/"),
		publicBase: strings.TrimRight(cfg.PublicBase, "/"),
		useBaseURL: cfg.PublicBase != "",
	}
	return store, nil
}

func loadAWSConfig(ctx context.Context, cfg S3Config) (aws.Config, error) {
	options := []func(*config.LoadOptions) error{}
	if cfg.Region != "" {
		options = append(options, config.WithRegion(cfg.Region))
	}
	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		options = append(options, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, cfg.SessionToken),
		))
	}
	return config.LoadDefaultConfig(ctx, options...)
}

func (s *S3Store) Save(ctx context.Context, id, filename string, data []byte) (string, error) {
	if id == "" {
		return "", fmt.Errorf("asset id is required")
	}
	key := s.objectKey(id, filename, data)
	contentType := http.DetectContentType(data)
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("s3 put object: %w", err)
	}
	return s.publicURL(key), nil
}

func (s *S3Store) objectKey(id, filename string, data []byte) string {
	ext := assetExtension(filename, data)
	name := id + ext
	if s.prefix == "" {
		return name
	}
	return path.Join(s.prefix, name)
}

func (s *S3Store) publicURL(key string) string {
	if s.useBaseURL {
		return s.publicBase + "/" + key
	}
	return fmt.Sprintf("https://%s.s3.amazonaws.com/%s", s.bucket, key)
}
