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

	// Track consecutive violations of strict limit (5 msg/s)
	strictViolations map[string]int
	lastViolationSec map[string]int64

	// History for loop detection: threadID -> map[agentID]count
	threadHistory map[string]map[string]int

	mu sync.Mutex
}

func (g *Guardian) Start(ctx context.Context) error {
	g.stats = make(map[string][]time.Time)
	g.strictViolations = make(map[string]int)
	g.lastViolationSec = make(map[string]int64)
	g.threadHistory = make(map[string]map[string]int)

	_, err := g.NC.Subscribe("board.>", func(m *nats.Msg) {
		// 0. Payload Size Check (100MB)
		if len(m.Data) > 100*1024*1024 {
			log.Printf("Guardian: Payload too large (%d bytes). Blocking for 1h.", len(m.Data))
			// We don't have agentID yet, but we can't parse huge JSON anyway.
			return
		}

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

	// 1. Minutely Limit (60 msg/min)
	if msgCountLastMin > 60 {
		log.Printf("Guardian: Rate limit exceeded for agent %s (%d msg/min). Revoking for 10s.", agentID, msgCountLastMin)
		g.AuthProvider.RevokeAgent(agentID, 10*time.Second)
		return
	}

	// 2. Burst Limit (20 msg/sec)
	if msgCountLastSec > 20 {
		log.Printf("Guardian: Burst limit exceeded for agent %s (%d msg/sec). Revoking for 10s.", agentID, msgCountLastSec)
		g.AuthProvider.RevokeAgent(agentID, 10*time.Second)
		return
	}

	// 3. Strict Mode (5 msg/sec)
	// If it exceeds 5 msg/sec in consecutive seconds, block.
	if msgCountLastSec > 5 {
		currentSec := now.Unix()
		if g.lastViolationSec[agentID] != currentSec {
			// This is a new second that exceeds 5 msg/s
			if g.lastViolationSec[agentID] == currentSec-1 {
				g.strictViolations[agentID]++
			} else {
				g.strictViolations[agentID] = 1
			}
			g.lastViolationSec[agentID] = currentSec

			if g.strictViolations[agentID] >= 2 {
				log.Printf("Guardian: Strict rate limit exceeded for agent %s (continual > 5 msg/sec). Revoking for 10s.", agentID)
				g.AuthProvider.RevokeAgent(agentID, 10*time.Second)
				g.strictViolations[agentID] = 0 // Reset
			}
		}
	} else {
		// Reset violations if a second passes with <= 5 msgs
		currentSec := now.Unix()
		if g.lastViolationSec[agentID] < currentSec {
			g.strictViolations[agentID] = 0
		}
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
