package storage

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Expose generatePolicyJSON for testing by wrapping it if needed, or since it's same package we can just call it
func TestGeneratePolicyJSON(t *testing.T) {
	client := &AdminClient{Bucket: "test-bucket"}

	t.Run("UT-S3-001: worker can write to own thread output", func(t *testing.T) {
		policyBytes, err := client.generatePolicyJSON("worker", "thread-123")
		assert.NoError(t, err)

		var doc policyDocument
		err = json.Unmarshal(policyBytes, &doc)
		assert.NoError(t, err)

		assert.Len(t, doc.Statement, 2)

		// Statement 0 should allow PutObject to output
		assert.Contains(t, doc.Statement[0].Action, "s3:PutObject")
		assert.Contains(t, doc.Statement[0].Resource[0], "tasks/thread-123/output/*")
	})

	t.Run("UT-S3-002: observer can read", func(t *testing.T) {
		policyBytes, err := client.generatePolicyJSON("observer", "thread-123")
		assert.NoError(t, err)

		var doc policyDocument
		err = json.Unmarshal(policyBytes, &doc)
		assert.NoError(t, err)

		assert.Len(t, doc.Statement, 1)
		assert.Contains(t, doc.Statement[0].Action, "s3:GetObject")
		assert.NotContains(t, doc.Statement[0].Action, "s3:PutObject")
		assert.Contains(t, doc.Statement[0].Resource[0], "tasks/thread-123/*")
	})

	t.Run("UT-S3-103: path traversal rejection", func(t *testing.T) {
		_, err := client.generatePolicyJSON("worker", "../../etc/passwd")
		assert.Error(t, err)
		assert.True(t, strings.Contains(err.Error(), "path traversal"))
	})
}
