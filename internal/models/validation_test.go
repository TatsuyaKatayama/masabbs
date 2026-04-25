package models

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
)

func TestMessageValidation(t *testing.T) {
	validULID := ulid.Make().String()

	tests := []struct {
		name    string
		id      string
		env     MessageEnvelope
		wantErr error
	}{
		{
			name: "UT-VAL-001: Correct JSON schema message",
			id:   "UT-VAL-001",
			env: MessageEnvelope{
				Type:      "task",
				ThreadID:  &validULID,
				From:      "agent1",
				To:        []string{"agent2"},
				Timestamp: time.Now().Unix(),
				Payload:   json.RawMessage(`{"command":"echo"}`),
			},
			wantErr: nil,
		},
		{
			name: "UT-VAL-101: Missing 'to' field",
			id:   "UT-VAL-101",
			env: MessageEnvelope{
				Type:      "task",
				ThreadID:  &validULID,
				From:      "agent1",
				Payload:   json.RawMessage(`{"command":"echo"}`),
			},
			wantErr: ErrMissingTo,
		},
		{
			name: "UT-VAL-102: Null payload",
			id:   "UT-VAL-102",
			env: MessageEnvelope{
				Type:      "task",
				ThreadID:  &validULID,
				From:      "agent1",
				To:        []string{"agent2"},
				Payload:   json.RawMessage(`null`),
			},
			wantErr: ErrNullPayload,
		},
		{
			name: "UT-VAL-105: Invalid type",
			id:   "UT-VAL-105",
			env: MessageEnvelope{
				Type:      "unknown_type",
				ThreadID:  &validULID,
				From:      "agent1",
				To:        []string{"agent2"},
				Payload:   json.RawMessage(`{"command":"echo"}`),
			},
			wantErr: ErrInvalidType,
		},
		{
			name: "UT-VAL-107: ThreadID is UUID (not ULID)",
			id:   "UT-VAL-107",
			env: MessageEnvelope{
				Type:      "task",
				ThreadID:  strPtr("123e4567-e89b-12d3-a456-426614174000"),
				From:      "agent1",
				To:        []string{"agent2"},
				Payload:   json.RawMessage(`{"command":"echo"}`),
			},
			wantErr: ErrInvalidThreadID,
		},
		{
			name: "UT-VAL-109: ThreadID is empty string",
			id:   "UT-VAL-109",
			env: MessageEnvelope{
				Type:      "task",
				ThreadID:  strPtr(""),
				From:      "agent1",
				To:        []string{"agent2"},
				Payload:   json.RawMessage(`{"command":"echo"}`),
			},
			wantErr: ErrInvalidThreadID,
		},
		{
			name: "UT-VAL-110: Payload exceeds 10MB",
			id:   "UT-VAL-110",
			env: MessageEnvelope{
				Type:      "task",
				ThreadID:  &validULID,
				From:      "agent1",
				To:        []string{"agent2"},
				Payload:   json.RawMessage(strings.Repeat("a", 10*1024*1024+1)),
			},
			wantErr: ErrPayloadTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.env.Validate()
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr, "Expected error matching: %v", tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func strPtr(s string) *string {
	return &s
}
