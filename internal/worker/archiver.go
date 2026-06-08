package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/TatsuyaKatayama/masabbs/internal/auth"
	"github.com/TatsuyaKatayama/masabbs/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
)

type Archiver struct {
	DB   *pgxpool.Pool
	JS   jetstream.JetStream
	Auth *auth.Provider
}

// Start runs the archiver to consume messages from multiple streams
// and persist them into the PostgreSQL 'tasks' table.
func (a *Archiver) Start(ctx context.Context) error {
	// Matches names in internal/nats/jetstream.go
	streams := []string{"board_tasks", "board_events"}

	for _, streamName := range streams {
		// 1. Ensure a unique durable consumer for each stream
		consumer, err := a.JS.CreateOrUpdateConsumer(ctx, streamName, jetstream.ConsumerConfig{
			Durable:       fmt.Sprintf("server-archiver-%s", streamName),
			Description:   fmt.Sprintf("Server archiver for %s", streamName),
			AckPolicy:     jetstream.AckExplicitPolicy,
			DeliverPolicy: jetstream.DeliverAllPolicy,
		})
		if err != nil {
			return fmt.Errorf("failed to create consumer for %s: %w", streamName, err)
		}

		// 2. Start consuming
		go func(name string, c jetstream.Consumer) {
			log.Printf("Archiver started listening to stream: %s", name)

			consumeCtx, err := c.Consume(func(msg jetstream.Msg) {
				a.processMessage(msg)
			})
			if err != nil {
				log.Printf("Archiver error on stream %s: %v", name, err)
				return
			}

			<-ctx.Done()
			consumeCtx.Stop()
			log.Printf("Archiver stopped for stream: %s", name)

		}(streamName, consumer)
	}

	return nil
}

func (a *Archiver) processMessage(msg jetstream.Msg) {
	var env models.MessageEnvelope
	if err := json.Unmarshal(msg.Data(), &env); err != nil {
		log.Printf("Archiver failed to parse message: %v", err)
		msg.Ack()
		return
	}

	if err := env.Validate(); err != nil {
		log.Printf("Archiver: Invalid message from %s: %v", env.From, err)
		msg.Ack()
		return
	}

	// Signature Verification
	if a.Auth != nil && os.Getenv("SKIP_SIG_VERIFY") != "true" {
		// Verify impersonation
		// We use a map to ensure canonical JSON (sorted keys) for verification.
		var envMap map[string]interface{}
		if err := json.Unmarshal(msg.Data(), &envMap); err != nil {
			log.Printf("Archiver: Failed to unmarshal to map: %v", err)
			msg.Ack()
			return
		}
		sig := env.Signature
		delete(envMap, "signature")
		canonicalData, _ := json.Marshal(envMap) // Go sorts map keys!

		if err := a.Auth.VerifySignature(env.From, canonicalData, sig); err != nil {
			log.Printf("Archiver: Invalid signature from %s: %v", env.From, err)
			msg.Ack()
			return
		}

		// Safety check: Is this agent blocked?
		if a.Auth.IsRevoked(env.From) {
			log.Printf("Archiver: Dropping message from revoked agent %s", env.From)
			msg.Ack()
			return
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var err error

	// Thread Check & Idempotency
	if env.ThreadID != nil && *env.ThreadID != "" {
		var threadStatus string
		err = a.DB.QueryRow(ctx, "SELECT status FROM threads WHERE id = $1", *env.ThreadID).Scan(&threadStatus)
		if err != nil {
			log.Printf("Archiver: Thread %s not found. Dropping message.", *env.ThreadID)
			msg.Ack()
			return
		}

		// Discard late messages if thread is already finished
		if threadStatus == "done" || threadStatus == "error" {
			log.Printf("Archiver: Thread %s is in status %s. Dropping message type %s from %s.", *env.ThreadID, threadStatus, env.Type, env.From)
			msg.Ack()
			return
		}

		// Rejection based on Thread Status
		if env.Type == "assign" && threadStatus == "assigned" {
			log.Printf("Archiver: Thread %s is already assigned. Dropping duplicate assignment from %s.", *env.ThreadID, env.From)
			msg.Ack()
			return
		}

		// Idempotency Check for 'result' from the same agent
		if env.Type == "result" {
			var exists bool
			err = a.DB.QueryRow(ctx, `
				SELECT EXISTS(
					SELECT 1 FROM tasks 
					WHERE thread_id = $1 AND agent_id = $2 AND type = 'result'
				)
			`, env.ThreadID, env.From).Scan(&exists)

			if err == nil && exists {
				msg.Ack()
				return
			}
		}
	}

	taskID := ulid.Make().String()

	_, err = a.DB.Exec(ctx, `
		INSERT INTO tasks (id, thread_id, agent_id, type, to_agents, observers, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, taskID, env.ThreadID, env.From, env.Type, env.To, env.Observers, env.Payload)

	if err != nil {
		log.Printf("Archiver failed to persist message to DB: %v", err)
		// 3. Backoff to avoid tight retry loop on DB error
		msg.NakWithDelay(5 * time.Second)
		return
	}

	msg.Ack()
}
