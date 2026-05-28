package storage

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type StorageProvider interface {
	GetThreadInputPath(threadID string) string
	CreateThreadFolders(ctx context.Context, threadID string) error
	ListFiles(ctx context.Context, prefix string) ([]string, error)
	GetPresignedURL(ctx context.Context, objectKey string) (string, error)
}

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

// GetThreadInputPath returns the S3 relative path for a thread's input directory
func (c *Client) GetThreadInputPath(threadID string) string {
	return fmt.Sprintf("tasks/%s/input/", threadID)
}

// generateThreadPath returns the base path for a thread
func (c *Client) generateThreadPath(threadID string) string {
	return fmt.Sprintf("tasks/%s/", threadID)
}

// ListFiles lists all objects under a given prefix in the bucket
func (c *Client) ListFiles(ctx context.Context, prefix string) ([]string, error) {
	var files []string
	objectCh := c.S3.ListObjects(ctx, c.Bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})

	for object := range objectCh {
		if object.Err != nil {
			return nil, object.Err
		}
		// Skip placeholder files
		if strings.HasSuffix(object.Key, ".keep") {
			continue
		}
		files = append(files, object.Key)
	}
	return files, nil
}

// GetPresignedURL generates a temporary URL for downloading an object
func (c *Client) GetPresignedURL(ctx context.Context, objectKey string) (string, error) {
	// Set expiration to 1 hour
	expiry := time.Second * 3600
	presignedURL, err := c.S3.PresignedGetObject(ctx, c.Bucket, objectKey, expiry, nil)
	if err != nil {
		return "", err
	}
	return presignedURL.String(), nil
}

// CreateThreadFolders creates the initial structure for a new thread (input, output, logs, and meta.json)
// Caller must also invoke MinIO Admin API to apply access controls (SetThreadPolicy).
func (c *Client) CreateThreadFolders(ctx context.Context, threadID string) error {
	basePath := c.generateThreadPath(threadID)

	// Create subdirectories with placeholder files
	folders := []string{"input/", "output/", "logs/"}
	for _, folder := range folders {
		objectName := basePath + folder + ".keep"
		_, err := c.S3.PutObject(ctx, c.Bucket, objectName, bytes.NewReader([]byte{}), 0, minio.PutObjectOptions{})
		if err != nil {
			return fmt.Errorf("failed to create folder %s: %w", folder, err)
		}
	}

	// Create meta.json (spec v1.0.1)
	metaName := basePath + "meta.json"
	_, err := c.S3.PutObject(ctx, c.Bucket, metaName, bytes.NewReader([]byte("{}")), 2, minio.PutObjectOptions{
		ContentType: "application/json",
	})
	if err != nil {
		return fmt.Errorf("failed to create meta.json: %w", err)
	}

	return nil
}
