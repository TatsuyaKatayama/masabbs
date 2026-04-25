package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/minio/madmin-go/v3"
)

type AdminClient struct {
	MAdmin *madmin.AdminClient
	Bucket string
}

// NewAdminClient initializes a MinIO Admin client
func NewAdminClient(cfg Config) (*AdminClient, error) {
	madminClient, err := madmin.New(cfg.Endpoint, cfg.AccessKeyID, cfg.SecretAccessKey, cfg.UseSSL)
	if err != nil {
		return nil, fmt.Errorf("failed to create madmin client: %w", err)
	}

	return &AdminClient{
		MAdmin: madminClient,
		Bucket: cfg.Bucket,
	}, nil
}

// IAM Policy structures for safe JSON generation
type policyStatement struct {
	Effect   string   `json:"Effect"`
	Action   []string `json:"Action"`
	Resource []string `json:"Resource"`
}

type policyDocument struct {
	Version   string            `json:"Version"`
	Statement []policyStatement `json:"Statement"`
}

// generatePolicyJSON generates the JSON policy string based on role using encoding/json
func (c *AdminClient) generatePolicyJSON(role, threadID string) ([]byte, error) {
	doc := policyDocument{
		Version: "2012-10-17",
	}

	switch role {
	case "worker":
		doc.Statement = []policyStatement{
			{
				Effect: "Allow",
				Action: []string{"s3:PutObject", "s3:GetObject"},
				Resource: []string{
					fmt.Sprintf("arn:aws:s3:::%s/tasks/%s/output/*", c.Bucket, threadID),
				},
			},
			{
				Effect: "Allow",
				Action: []string{"s3:GetObject"},
				Resource: []string{
					fmt.Sprintf("arn:aws:s3:::%s/tasks/%s/input/*", c.Bucket, threadID),
				},
			},
		}
	case "observer":
		doc.Statement = []policyStatement{
			{
				Effect: "Allow",
				Action: []string{"s3:GetObject"},
				Resource: []string{
					fmt.Sprintf("arn:aws:s3:::%s/tasks/%s/*", c.Bucket, threadID),
				},
			},
		}
	default:
		return nil, fmt.Errorf("unsupported role for dynamic policy: %s", role)
	}

	return json.Marshal(doc)
}

// SetThreadPolicy creates and attaches a policy for a specific thread to an agent.
// agentID is expected to be an existing MinIO user access key.
func (c *AdminClient) SetThreadPolicy(ctx context.Context, threadID, agentID, role string) error {
	policyBytes, err := c.generatePolicyJSON(role, threadID)
	if err != nil {
		return fmt.Errorf("failed to generate policy JSON: %w", err)
	}

	policyName := fmt.Sprintf("policy-%s-%s", threadID, agentID)

	// 1. Create/Update the canned policy in MinIO
	err = c.MAdmin.AddCannedPolicy(ctx, policyName, policyBytes)
	if err != nil {
		return fmt.Errorf("failed to add canned policy %s: %w", policyName, err)
	}

	// 2. Attach the policy to the agent (MinIO user)
	// isGroup = false means attach to user
	err = c.MAdmin.SetPolicy(ctx, policyName, agentID, false)
	if err != nil {
		return fmt.Errorf("failed to attach policy %s to user %s: %w", policyName, agentID, err)
	}

	return nil
}

// RemoveThreadPolicy detaches and removes the thread-specific policy from MinIO.
// This should be called when a task is finished or the thread is closed.
func (c *AdminClient) RemoveThreadPolicy(ctx context.Context, threadID, agentID string) error {
	policyName := fmt.Sprintf("policy-%s-%s", threadID, agentID)

	// 1. Detach policy from user by setting an empty policy (or just deleting the policy name from user)
	// In MinIO, removing the canned policy will effectively detach it, 
	// but it's cleaner to explicitly clear it if needed.
	err := c.MAdmin.SetPolicy(ctx, "", agentID, false)
	if err != nil {
		log := fmt.Sprintf("warning: failed to clear policy for user %s: %v", agentID, err)
		fmt.Println(log)
	}

	// 2. Delete the canned policy
	err = c.MAdmin.RemoveCannedPolicy(ctx, policyName)
	if err != nil {
		return fmt.Errorf("failed to remove canned policy %s: %w", policyName, err)
	}

	return nil
}
