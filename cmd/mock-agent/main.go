package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/TatsuyaKatayama/masabbs/internal/models"
	"github.com/nats-io/nats.go"
)

func main() {
	natsURL := getEnv("NATS_URL", "nats://localhost:4222")
	agentID := getEnv("AGENT_ID", "mock-agent-1")
	agentRole := getEnv("AGENT_ROLE", "worker")

	nc, err := nats.Connect(natsURL)
	if err != nil {
		log.Fatal(err)
	}
	defer nc.Close()

	log.Printf("Mock Agent [%s] (%s) started\n", agentID, agentRole)

	// 1. Send initial status
	sendStatus(nc, agentID, "online", 0)

	// 2. Periodic status updates
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for range ticker.C {
			sendStatus(nc, agentID, "online", 0)
		}
	}()

	// 3. Subscribe to tasks
	nc.Subscribe("board.task.*", func(m *nats.Msg) {
		var env models.MessageEnvelope
		if err := json.Unmarshal(m.Data, &env); err != nil {
			return
		}

		log.Printf("Received task: %s from %s\n", *env.ThreadID, env.From)
		
		// Respond with offer
		time.Sleep(1 * time.Second)
		sendOffer(nc, agentID, *env.ThreadID)
	})

	// 4. Subscribe to assignments
	nc.Subscribe(fmt.Sprintf("board.assign.%s", "*"), func(m *nats.Msg) {
		var env models.MessageEnvelope
		if err := json.Unmarshal(m.Data, &env); err != nil {
			return
		}

		// Check if assigned to me
		isMe := false
		for _, to := range env.To {
			if to == agentID {
				isMe = true
				break
			}
		}

		if isMe {
			log.Printf("Task assigned to me! Thread: %s\n", *env.ThreadID)
			handleTask(nc, agentID, *env.ThreadID)
		}
	})

	// Wait for termination
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Println("Mock Agent shutting down...")
}

func sendStatus(nc *nats.Conn, agentID, state string, progress int) {
	payload, _ := json.Marshal(models.StatusPayload{
		Progress: progress,
		State:    state,
	})
	env := models.MessageEnvelope{
		Type:      "status",
		From:      agentID,
		Timestamp: time.Now().Unix(),
		Payload:   payload,
	}
	data, _ := json.Marshal(env)
	nc.Publish(fmt.Sprintf("board.status.%s", agentID), data)
}

func sendOffer(nc *nats.Conn, agentID, threadID string) {
	payload, _ := json.Marshal(models.OfferPayload{
		ETASeconds: 10,
		Confidence: 0.95,
	})
	env := models.MessageEnvelope{
		Type:      "offer",
		ThreadID:  &threadID,
		From:      agentID,
		Timestamp: time.Now().Unix(),
		Payload:   payload,
	}
	data, _ := json.Marshal(env)
	nc.Publish(fmt.Sprintf("board.offer.%s", threadID), data)
	log.Printf("Sent offer for thread: %s\n", threadID)
}

func handleTask(nc *nats.Conn, agentID, threadID string) {
	// Simulate processing
	sendStatus(nc, agentID, "running", 20)
	time.Sleep(2 * time.Second)
	sendStatus(nc, agentID, "running", 60)
	time.Sleep(2 * time.Second)
	sendStatus(nc, agentID, "running", 100)

	// Send result
	payload, _ := json.Marshal(models.ResultPayload{
		OutputDir: fmt.Sprintf("tasks/%s/output/", threadID),
		ExitCode:  0,
	})
	env := models.MessageEnvelope{
		Type:      "result",
		ThreadID:  &threadID,
		From:      agentID,
		Timestamp: time.Now().Unix(),
		Payload:   payload,
	}
	data, _ := json.Marshal(env)
	nc.Publish(fmt.Sprintf("board.result.%s", threadID), data)
	log.Printf("Sent result for thread: %s\n", threadID)

	sendStatus(nc, agentID, "online", 0)
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
