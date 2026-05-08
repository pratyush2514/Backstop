package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type StorageConfig struct {
	Bucket   string
	Endpoint string
	Prefix   string
}

func ParseStorage(raw string, fallbackEndpoint string, prefix string) (StorageConfig, error) {
	if !strings.HasPrefix(raw, "s3://") {
		return StorageConfig{}, fmt.Errorf("storage must start with s3://")
	}
	trimmed := strings.TrimPrefix(raw, "s3://")
	parts := strings.SplitN(trimmed, "@", 2)
	bucket := strings.TrimSpace(parts[0])
	if bucket == "" {
		return StorageConfig{}, fmt.Errorf("storage bucket is required")
	}

	endpoint := fallbackEndpoint
	if len(parts) == 2 {
		endpoint = strings.TrimSpace(parts[1])
	}
	if endpoint != "" {
		if _, err := url.ParseRequestURI(endpoint); err != nil {
			return StorageConfig{}, fmt.Errorf("invalid storage endpoint: %w", err)
		}
	}

	return StorageConfig{Bucket: bucket, Endpoint: endpoint, Prefix: strings.Trim(prefix, "/")}, nil
}

func NewS3Client(ctx context.Context, storage StorageConfig) (*s3.Client, error) {
	if storage.Endpoint == "" {
		cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
		if err != nil {
			return nil, err
		}
		return s3.NewFromConfig(cfg), nil
	}

	cfg, err := config.LoadDefaultConfig(
		ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			envOrDefault("AWS_ACCESS_KEY_ID", "minioadmin"),
			envOrDefault("AWS_SECRET_ACCESS_KEY", "minioadmin"),
			"",
		)),
	)
	if err != nil {
		return nil, err
	}

	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(storage.Endpoint)
		o.UsePathStyle = true
	}), nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
