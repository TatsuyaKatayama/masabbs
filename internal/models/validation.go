package models

import (
	"encoding/json"
	"errors"
	"fmt"
)

var (
	ErrNullPayload     = errors.New("payload cannot be null")
	ErrInvalidType     = errors.New("invalid message type")
	ErrInvalidThreadID = errors.New("invalid thread_id format (must be ULID)")
	ErrMissingAgentID  = errors.New("missing 'from' agent_id")
	ErrPayloadTooLarge = errors.New("payload exceeds 10MB")
)

var ValidTypes = map[string]bool{
	"task": true, "offer": true, "assign": true, "result": true,
	"event": true,
}

// Validate checks if the MessageEnvelope is valid according to the specification
func (e *MessageEnvelope) Validate() error {
	if e.Type == "" || !ValidTypes[e.Type] {
		return ErrInvalidType
	}
	if e.From == "" {
		return ErrMissingAgentID
	}
	if e.ThreadID != nil {
		if *e.ThreadID == "" {
			return ErrInvalidThreadID
		}
	} else if e.Type != "event" {
		// ThreadID is required for task, offer, assign, result
		return ErrInvalidThreadID
	}

	if len(e.Payload) == 0 || string(e.Payload) == "null" {
		return ErrNullPayload
	}

	if len(e.Payload) > 10*1024*1024 { // 10MB limit
		return ErrPayloadTooLarge
	}

	// Validate JSON payload
	if !json.Valid(e.Payload) {
		return fmt.Errorf("invalid json payload")
	}

	return nil
}
