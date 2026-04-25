package nats

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nkeys"
)

type Client struct {
	NC *nats.Conn
	JS jetstream.JetStream
}

// Config holds NATS connection settings
type Config struct {
	URL      string
	NKeySeed string // Seed starting with 'S'
	JWT      string // User JWT
}

// Connect establishes a connection to NATS and initializes JetStream
func Connect(cfg Config) (*Client, error) {
	opts := []nats.Option{
		nats.Name("masabbs-server"),
		nats.Timeout(10 * time.Second),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),             // Infinite reconnects for a daemon
		nats.ReconnectWait(2 * time.Second), // Wait 2s between attempts
	}

	// Setup NKey + JWT Authentication
	if cfg.NKeySeed != "" && cfg.JWT != "" {
		kp, err := nkeys.FromSeed([]byte(cfg.NKeySeed))
		if err != nil {
			return nil, fmt.Errorf("failed to parse nkey seed: %w", err)
		}

		authOpt := nats.UserJWT(
			func() (string, error) { return cfg.JWT, nil },
			func(nonce []byte) ([]byte, error) { return kp.Sign(nonce) },
		)
		opts = append(opts, authOpt)
	}

	nc, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize JetStream: %w", err)
	}

	client := &Client{
		NC: nc,
		JS: js,
	}

	// Setup streams according to spec 5.2
	if err := client.setupStreams(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to setup streams: %w", err)
	}

	return client, nil
}

func (c *Client) setupStreams(ctx context.Context) error {
	streams := []jetstream.StreamConfig{
		{
			Name:        "board_tasks",
			Description: "Task lifecycle (task, offer, assign, result)",
			Subjects: []string{
				"board.task.*",
				"board.offer.*",
				"board.assign.*",
				"board.result.*",
			},
			Retention: jetstream.LimitsPolicy,
			MaxAge:    7 * 24 * time.Hour, // 7 days
		},
		{
			Name:        "board_status",
			Description: "High frequency agent status updates",
			Subjects: []string{
				"board.status.*",
			},
			Retention: jetstream.LimitsPolicy,
			MaxAge:    24 * time.Hour, // 1 day
		},
		{
			Name:        "board_events",
			Description: "General system events",
			Subjects: []string{
				"board.event.*",
			},
			Retention: jetstream.LimitsPolicy,
			MaxAge:    3 * 24 * time.Hour, // 3 days
		},
		{
			Name:        "board_shutdown",
			Description: "Shutdown commands (WorkQueue)",
			Subjects: []string{
				"board.shutdown.*",
			},
			Retention: jetstream.WorkQueuePolicy,
			MaxAge:    24 * time.Hour, // 1 day
		},
	}

	for _, cfg := range streams {
		cfg.Replicas = 1 // 1 for dev, 3 for prod
		_, err := c.JS.CreateOrUpdateStream(ctx, cfg)
		if err != nil {
			return fmt.Errorf("failed to create stream %s: %w", cfg.Name, err)
		}
	}

	log.Println("NATS JetStream streams (tasks, status, events, shutdown) configured successfully.")
	return nil
}

func (c *Client) Close() {
	if c.NC != nil {
		c.NC.Drain()
	}
}
