package worker

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/TatsuyaKatayama/masabbs/internal/auth"
	"github.com/TatsuyaKatayama/masabbs/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
)

type Guardian struct {
	AuthProvider *auth.Provider
	NC           *nats.Conn
	DB           *pgxpool.Pool

	// Stats for rate limiting: agentID -> []timestamps
	stats map[string][]time.Time
	
	// History for loop detection: threadID -> map[agentID]count
	threadHistory map[string]map[string]int

	mu sync.Mutex
}

func (g *Guardian) Start(ctx context.Context) error {
	g.stats = make(map[string][]time.Time)
	g.threadHistory = make(map[string]map[string]int)

	_, err := g.NC.Subscribe("board.>", func(m *nats.Msg) {
		var env models.MessageEnvelope
		if err := json.Unmarshal(m.Data, &env); err != nil {
			return
		}

		if env.From == "" {
			return
		}

		// 1. Rate Limit Check
		g.checkRateLimit(env.From)

		// 2. Loop Detection Check
		if env.ThreadID != nil && *env.ThreadID != "" {
			g.checkLoopDetection(ctx, *env.ThreadID, env.From)
		}
	})
	if err != nil {
		return err
	}

	log.Println("Guardian started: monitoring message rates and loops on board.>")
	<-ctx.Done()
	return nil
}

func (g *Guardian) checkRateLimit(agentID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	g.stats[agentID] = append(g.stats[agentID], now)

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

	if msgCountLastMin > 60 || msgCountLastSec > 5 {
		log.Printf("Guardian: Rate limit exceeded for agent %s (%d msg/min, %d msg/sec). Revoking for 10s.", agentID, msgCountLastMin, msgCountLastSec)
		g.AuthProvider.RevokeAgent(agentID, 10*time.Second)
	}
}

func (g *Guardian) checkLoopDetection(ctx context.Context, threadID string, agentID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if _, ok := g.threadHistory[threadID]; !ok {
		g.threadHistory[threadID] = make(map[string]int)
	}

	g.threadHistory[threadID][agentID]++

	// If the same agent participates in the same thread more than 2 times,
	// we consider it a likely infinite loop / circular dependency (a->b->c->a->...).
	if g.threadHistory[threadID][agentID] > 2 {
		log.Printf("Guardian: Loop detected in thread %s involving agent %s. Marking thread as error.", threadID, agentID)
		
		if g.DB != nil {
			// Do not block the mutex while querying DB
			go func(tid string) {
				tCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_, err := g.DB.Exec(tCtx, "UPDATE threads SET status = 'error' WHERE id = $1", tid)
				if err != nil {
					log.Printf("Guardian failed to update thread status to error: %v", err)
				}
			}(threadID)
		}
		
		// Reset history so we don't spam DB updates
		delete(g.threadHistory, threadID)
	}
}
