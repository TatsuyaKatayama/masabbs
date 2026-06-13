package mentions

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Note: This test requires a running DB or should be mocked.
// For simplicity in this environment, I'll mock the Resolver if possible,
// but since it's a small unit, I'll check if I can run it against a real DB if available.
// Actually, let's just do a basic test and skip if DB is not ready.

func TestExtractMentions(t *testing.T) {
	text := "Hello @agent-1 and @team. @agent-1 again."
	res := ExtractMentions(text)

	assert.Contains(t, res, "agent-1")
	assert.Contains(t, res, "team")
	assert.Equal(t, 2, len(res))
}

// Full Resolve testing usually happens in integration tests because it depends on DB.
