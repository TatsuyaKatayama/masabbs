package storage

import (
	"bytes"
	"context"
	"fmt"
	"log"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Client struct {
	S3     *minio.Client
	Bucket string
}

type Config struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          bool
	Bucket          string
}

// NewClient initializes a MinIO client and ensures the target bucket exists
func NewClient(cfg Config) (*Client, error) {
	minioClient, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create minio client: %w", err)
	}

	client := &Client{
		S3:     minioClient,
		Bucket: cfg.Bucket,
	}

	// Ensure bucket exists
	err = client.ensureBucket(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to ensure bucket: %w", err)
	}

	return client, nil
}

func (c *Client) ensureBucket(ctx context.Context) error {
	exists, err := c.S3.BucketExists(ctx, c.Bucket)
	if err != nil {
		return err
	}

	if !exists {
		err = c.S3.MakeBucket(ctx, c.Bucket, minio.MakeBucketOptions{})
		if err != nil {
			return err
		}
		log.Printf("Bucket '%s' created successfully.", c.Bucket)
	}
	return nil
}

// generateThreadPath returns the base path for a thread
func (c *Client) generateThreadPath(threadID string) string {
	return fmt.Sprintf("tasks/%s/", threadID)
}

// CreateThreadFolders creates the initial structure for a new thread (input, output, logs)
// Caller must also invoke MinIO Admin API to apply access controls (SetThreadPolicy).
func (c *Client) CreateThreadFolders(ctx context.Context, threadID string) error {
	folders := []string{"input/", "output/", "logs/"}
	basePath := c.generateThreadPath(threadID)

	for _, folder := range folders {
		objectName := basePath + folder + ".keep" // Create a placeholder file to ensure path exists
		_, err := c.S3.PutObject(ctx, c.Bucket, objectName, bytes.NewReader([]byte{}), 0, minio.PutObjectOptions{})
		if err != nil {
			return fmt.Errorf("failed to create folder %s: %w", folder, err)
		}
	}
	return nil
}
