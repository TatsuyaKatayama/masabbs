package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/TatsuyaKatayama/masabbs/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
)

type Archiver struct {
	DB *pgxpool.Pool
	JS jetstream.JetStream
}

// Start runs the archiver to consume messages from multiple streams
// and persist them into the PostgreSQL 'tasks' table.
func (a *Archiver) Start(ctx context.Context) error {
	// Matches names in internal/nats/jetstream.go
	streams := []string{"board_tasks", "board_status", "board_events"}

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
		msg.Ack() // Ack bad JSON to avoid poison pill
		return
	}

	taskID := ulid.Make().String()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := a.DB.Exec(ctx, `
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
