package models

import (
	"encoding/json"
	"time"
)

// Team represents a group of agents
type Team struct {
	ID          string    `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// Agent represents an autonomous participant in the system
type Agent struct {
	ID           string          `json:"id" db:"id"`
	Name         string          `json:"name" db:"name"`
	Role         string          `json:"role" db:"role"` // manager, worker, observer
	Tools        json.RawMessage `json:"tools" db:"tools"` // JSONB in DB
	Capabilities json.RawMessage `json:"capabilities" db:"capabilities"` // JSONB in DB
	Status       string          `json:"status" db:"status"` // online, offline, busy
	TeamID       *string         `json:"team_id,omitempty" db:"team_id"`
	CreatedAt    time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at" db:"updated_at"`
}

// AgentRelation defines hierarchical management between agents
type AgentRelation struct {
	ParentAgentID string `json:"parent_agent_id" db:"parent_agent_id"`
	ChildAgentID  string `json:"child_agent_id" db:"child_agent_id"`
	RelationType  string `json:"relation_type" db:"relation_type"`
}

// Thread represents a task thread
type Thread struct {
	ID             string    `json:"id" db:"id"`
	ParentThreadID *string   `json:"parent_thread_id,omitempty" db:"parent_thread_id"`
	CreatedByAgent string    `json:"created_by_agent" db:"created_by_agent"`
	AssignedAgent  *string   `json:"assigned_agent,omitempty" db:"assigned_agent"`
	Status         string    `json:"status" db:"status"` // open, assigned, processing, etc.
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" db:"updated_at"`
}

// Task represents a record in the 'tasks' table
type Task struct {
	ID        string          `json:"id" db:"id"`
	ThreadID  *string         `json:"thread_id,omitempty" db:"thread_id"`
	AgentID   string          `json:"agent_id" db:"agent_id"` // The 'from' field
	Type      string          `json:"type" db:"type"`
	ToAgents  []string        `json:"to_agents,omitempty" db:"to_agents"` // TEXT[] in DB
	Observers []string        `json:"observers,omitempty" db:"observers"` // TEXT[] in DB
	Payload   json.RawMessage `json:"payload" db:"payload"`               // JSONB in DB
	CreatedAt time.Time       `json:"created_at" db:"created_at"`
}

// MessageEnvelope is the common structure for NATS messages (for wire communication)
type MessageEnvelope struct {
	Type      string          `json:"type"`
	ThreadID  *string         `json:"thread_id,omitempty"` // Pointer for true omitempty
	From      string          `json:"from"`
	To        []string        `json:"to,omitempty"`
	Observers []string        `json:"observers,omitempty"`
	Timestamp int64           `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// Payloads for different message types (for unmarshaling MessageEnvelope.Payload)

type TaskPayload struct {
	Command  string `json:"command"`
	InputDir string `json:"input_dir"`
	Deadline string `json:"deadline"` // ISO8601 string
}

type OfferPayload struct {
	ETASeconds int     `json:"eta_seconds"`
	Confidence float64 `json:"confidence"`
}

type AssignPayload struct {
	Reason string `json:"reason,omitempty"`
}

type ResultPayload struct {
	OutputDir string `json:"output_dir"`
	ExitCode  int    `json:"exit_code"`
	Error     string `json:"error,omitempty"`
}

type StatusPayload struct {
	Progress int    `json:"progress"`
	State    string `json:"state"` // running, paused, error
}

type ShutdownPayload struct {
	Reason string `json:"reason"`
}

// TaskLog represents a log entry for a thread or agent
type TaskLog struct {
	ID        int64     `json:"id" db:"id"`
	ThreadID  *string   `json:"thread_id,omitempty" db:"thread_id"` // Nullable
	AgentID   string    `json:"agent_id" db:"agent_id"`
	Level     string    `json:"level" db:"level"` // info, warn, error
	Message   string    `json:"message" db:"message"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}
