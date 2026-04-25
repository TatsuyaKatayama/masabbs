package worker

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/TatsuyaKatayama/masabbs/internal/auth"
	"github.com/TatsuyaKatayama/masabbs/internal/models"
	"github.com/nats-io/nats.go"
)

type Guardian struct {
	AuthProvider *auth.Provider
	NC           *nats.Conn
	
	// Stats: agentID -> []timestamps
	stats map[string][]time.Time
	mu    sync.Mutex
}

func (g *Guardian) Start(ctx context.Context) error {
	g.stats = make(map[string][]time.Time)

	// We use a plain NATS subscription to monitor ALL traffic on board.>
	// This is more efficient for real-time monitoring than JetStream for this purpose.
	_, err := g.NC.Subscribe("board.>", func(m *nats.Msg) {
		var env models.MessageEnvelope
		if err := json.Unmarshal(m.Data, &env); err != nil {
			return
		}

		if env.From == "" {
			return
		}

		g.checkRateLimit(env.From)
	})
	if err != nil {
		return err
	}

	log.Println("Guardian started: monitoring message rates on board.>")
	<-ctx.Done()
	return nil
}

func (g *Guardian) checkRateLimit(agentID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	g.stats[agentID] = append(g.stats[agentID], now)

	// Cleanup old stats (older than 1 minute)
	var recent []time.Time
	oneMinAgo := now.Add(-1 * time.Minute)
	oneSecAgo := now.Add(-1 * time.Second)
	
	msgCountLastMin := 0
	msgCountLastSec := 0

	for _, t := range g.stats[agentID] {
		if t.After(oneMinAgo) {
			recent = append(recent, t)
			msgCountLastMin++
			if t.After(oneSecAgo) {
				msgCountLastSec++
			}
		}
	}
	g.stats[agentID] = recent

	// Check thresholds
	// E2E-ATK-001: 60 msg/min
	// E2E-ATK-002: 5 msg/sec
	if msgCountLastMin > 60 || msgCountLastSec > 5 {
		log.Printf("Guardian: Rate limit exceeded for agent %s (%d msg/min, %d msg/sec). Revoking for 10s.", agentID, msgCountLastMin, msgCountLastSec)
		g.AuthProvider.RevokeAgent(agentID, 10*time.Second)
	}
}
