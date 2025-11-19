package storage

import (
	"context"
	"fmt"

	"api/media/internal/config"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Client struct {
	client *s3.Client
	bucket string
}

func NewS3Client(ctx context.Context, cfg *config.Config) (*S3Client, error) {
	// Configure AWS SDK for Cloudflare R2
	r2Resolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL: fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.R2AccountID),
		}, nil
	})

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithEndpointResolverWithOptions(r2Resolver),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.R2AccessKeyID,
			cfg.R2SecretAccessKey,
			"",
		)),
		awsconfig.WithRegion("auto"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg)

	return &S3Client{
		client: client,
		bucket: cfg.R2Bucket,
	}, nil
}

func (s *S3Client) Client() *s3.Client {
	return s.client
}

func (s *S3Client) Bucket() string {
	return s.bucket
}
