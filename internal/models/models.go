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
	Mission     string    `json:"mission" db:"mission"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// Agent represents an autonomous participant in the system
type Agent struct {
	ID           string          `json:"id" db:"id"`
	Name         string          `json:"name" db:"name"`
	Role         string          `json:"role" db:"role"` // manager, worker, observer
	Mission      string          `json:"mission" db:"mission"`
	Tools        json.RawMessage `json:"tools" db:"tools"`               // JSONB in DB
	Capabilities json.RawMessage `json:"capabilities" db:"capabilities"` // JSONB in DB
	Status       string          `json:"status" db:"status"`             // online, offline, busy
	TeamID       *string         `json:"team_id,omitempty" db:"team_id"`
	UIPosX       float64         `json:"ui_pos_x" db:"ui_pos_x"`
	UIPosY       float64         `json:"ui_pos_y" db:"ui_pos_y"`
	CreatedAt    time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at" db:"updated_at"`
}

// AgentRelation defines directional relationships between agents in a team (v1.2.0)
type AgentRelation struct {
	ID               string `json:"id" db:"id"`
	TeamID           string `json:"team_id" db:"team_id"`
	SourceID         string `json:"source_id" db:"source_id"`
	TargetID         string `json:"target_id" db:"target_id"`
	SourceHandle     string `json:"source_handle" db:"source_handle"`
	TargetHandle     string `json:"target_handle" db:"target_handle"`
	RelationType     string `json:"relation_type" db:"relation_type"`         // leader, subordinate, consultant, reviewer
	RelationCategory string `json:"relation_category" db:"relation_category"` // vertical, horizontal
}

// TeamAgent represents many-to-many team membership for agents.
type TeamAgent struct {
	TeamID    string    `json:"team_id" db:"team_id"`
	AgentID   string    `json:"agent_id" db:"agent_id"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// NetworkMember represents an adjacent agent with a relative relation
type NetworkMember struct {
	AgentID      string          `json:"agent_id"`
	Relation     RelationInfo    `json:"relation"`
	Mission      string          `json:"mission"`
	Status       string          `json:"status"`
	Capabilities json.RawMessage `json:"capabilities"`
}

type RelationInfo struct {
	Category string `json:"category"`
	Type     string `json:"type"`
}

// TeamBlueprint represents the team structure for LLM understanding
type TeamBlueprint struct {
	TeamID           string  `json:"team_id"`
	StructureMermaid string  `json:"structure_mermaid"`
	Members          []Agent `json:"members"`
}

// ConfigurationSnapshot represents a snapshot of team and agent configurations (v1.3.0)
type ConfigurationSnapshot struct {
	Teams      []Team          `json:"teams"`
	Agents     []Agent         `json:"agents"`
	TeamAgents []TeamAgent     `json:"team_agents"`
	Relations  []AgentRelation `json:"relations"`
}

// ThreadSnapshot represents a replaceable snapshot of threads and their history.
type ThreadSnapshot struct {
	Threads []Thread  `json:"threads"`
	Tasks   []Task    `json:"tasks"`
	Logs    []TaskLog `json:"logs"`
}

// FullSnapshot represents a full backup of configuration and thread history.
type FullSnapshot struct {
	Teams      []Team          `json:"teams"`
	Agents     []Agent         `json:"agents"`
	TeamAgents []TeamAgent     `json:"team_agents"`
	Relations  []AgentRelation `json:"relations"`
	Threads    []Thread        `json:"threads"`
	Tasks      []Task          `json:"tasks"`
	Logs       []TaskLog       `json:"logs"`
	Configs    []Config        `json:"configs"`
}

// Config represents a saved configuration record in the database (v1.3.0)
type Config struct {
	ID          string          `json:"id" db:"id"`
	Name        string          `json:"name" db:"name"`
	Description string          `json:"description" db:"description"`
	Data        json.RawMessage `json:"data" db:"data"`
	CreatedAt   time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at" db:"updated_at"`
}

// Thread represents a task thread
type Thread struct {
	ID             string    `json:"id" db:"id"`
	ParentThreadID *string   `json:"parent_thread_id,omitempty" db:"parent_thread_id"`
	CreatedByAgent string    `json:"created_by_agent" db:"created_by_agent"`
	AssignedAgent  *string   `json:"assigned_agent,omitempty" db:"assigned_agent"`
	Status         string    `json:"status" db:"status"` // open, assigned, processing, etc.
	TeamID         *string   `json:"team_id,omitempty" db:"team_id"`
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
	ID        string          `json:"id,omitempty"`
	Type      string          `json:"type"`
	ThreadID  *string         `json:"thread_id,omitempty"` // Pointer for true omitempty
	From      string          `json:"from"`
	To        []string        `json:"to,omitempty"`
	Observers []string        `json:"observers,omitempty"`
	Timestamp int64           `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
	Signature string          `json:"signature,omitempty"`
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
	OutputDir string `json:"output_dir,omitempty"`
	ExitCode  int    `json:"exit_code"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
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
