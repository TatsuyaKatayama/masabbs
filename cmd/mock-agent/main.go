package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/TatsuyaKatayama/masabbs/internal/models"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

var (
	agentID   string
	agentRole string
	workDir   string
)

func main() {
	natsURL := getEnv("NATS_URL", "nats://localhost:4222")
	agentID = getEnv("AGENT_ID", "mock-expert-1")
	agentRole = getEnv("AGENT_ROLE", "worker")
	// Host SSD mount equivalent
	workDir = getEnv("WORK_DIR", "/tmp/masabbs-work")

	// Ensure local workbench exists
	if err := os.MkdirAll(fmt.Sprintf("%s/%s", workDir, agentID), 0755); err != nil {
		log.Fatalf("Failed to create local workbench: %v", err)
	}

	nc, err := nats.Connect(natsURL)
	if err != nil {
		log.Fatal(err)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Autonomous Mock Agent [%s] (%s) Booted. Workbench: %s\n", agentID, agentRole, workDir)

	// A. Initial Boot & Watch: Start infinite loop to monitor PENDING tasks (Pull Consumer)
	ctx := context.Background()
	consumer, err := js.CreateOrUpdateConsumer(ctx, "board_tasks", jetstream.ConsumerConfig{
		Durable:       fmt.Sprintf("worker-%s", agentID),
		FilterSubject: "board.task.*",
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       30 * time.Second, // Heartbeat window
	})
	if err != nil {
		log.Fatalf("Failed to create JetStream Pull Consumer: %v", err)
	}

	// Periodic status updates
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for range ticker.C {
			sendStatus(nc, "online", 0)
		}
	}()

	log.Println("Starting autonomous monitoring loop (check_board)...")

	// Autonomous Loop
	go autonomousLoop(nc, consumer)

	// Persistence & Q&A Simulation: Listen for direct queries about past threads
	nc.Subscribe(fmt.Sprintf("board.query.%s", agentID), func(m *nats.Msg) {
		handleQuery(nc, m.Data)
	})

	// Wait for termination
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Println("Agent shutting down. Preserving local workbench.")
}

// 1. check_board() & Fetch
func autonomousLoop(nc *nats.Conn, consumer jetstream.Consumer) {
	for {
		// Pull 1 message at a time
		msgs, err := consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
		if err != nil {
			if err == nats.ErrTimeout {
				continue // No tasks, keep watching
			}
			log.Printf("Fetch error: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		for msg := range msgs.Messages() {
			var env models.MessageEnvelope
			if err := json.Unmarshal(msg.Data(), &env); err != nil {
				log.Printf("Invalid message format: %v", err)
				msg.Ack()
				continue
			}

			// B. Status Update to PROCESSING (Implicit by Ack and status publish)
			msg.InProgress() // Extend Ack window
			sendStatus(nc, "busy", 10)
			
			log.Printf("Task acquired! Thread: %s", *env.ThreadID)

			// C. Execute Task Pipeline
			err := executeTask(nc, msg, *env.ThreadID)
			
			if err != nil {
				log.Printf("Task failed: %v", err)
				postResponse(nc, *env.ThreadID, "ERROR", fmt.Sprintf("Failed: %v", err))
				sendStatus(nc, "online", 0)
			} else {
				// D. Acknowledge NATS message upon SUCCESS
				msg.Ack()
				sendStatus(nc, "online", 0)
			}
		}
	}
}

// 2. Fetch (sync_from_s3) -> 3. Compute (run_simulation) -> 4. Export (sync_to_s3) -> 5. Publish
func executeTask(nc *nats.Conn, msg jetstream.Msg, threadID string) error {
	threadDir := fmt.Sprintf("%s/%s/%s", workDir, agentID, threadID)
	
	// Simulate: sync_from_s3(thread_id, "input/", target_dir)
	log.Printf("  [Skill] sync_from_s3: Downloading input for %s to %s", threadID, threadDir)
	time.Sleep(1 * time.Second) // Network latency simulation
	os.MkdirAll(threadDir+"/raw_data", 0755)
	
	// Simulate: run_simulation (Heartbeat required)
	log.Println("  [Skill] run_simulation: Computing massive data...")
	for i := 1; i <= 3; i++ {
		time.Sleep(2 * time.Second) // Heavy computation
		msg.InProgress() // Extend ACK (Heartbeat)
		sendStatus(nc, "busy", 10+i*20)
	}

	// Write massive pseudo-data to local SSD
	os.WriteFile(threadDir+"/raw_data/huge_simulation.bin", []byte("100GB of simulation data..."), 0644)
	
	// Simulate: generate_summary_plots()
	log.Println("  [Skill] generate_summary_plots: Extracting lightweight summary...")
	summaryPath := threadDir + "/summary.pdf"
	os.WriteFile(summaryPath, []byte("Lightweight PDF Summary"), 0644)

	// Simulate: sync_to_s3(thread_id, local_path)
	log.Printf("  [Skill] sync_to_s3: Uploading %s to /tasks/%s/output/", summaryPath, threadID)
	time.Sleep(1 * time.Second)

	// 5. Publish (post_response)
	postResponse(nc, threadID, "SUCCESS", "Simulation complete. Summary uploaded to output/summary.pdf")
	
	log.Println("  Task Completed. Local data retained for Q&A.")
	return nil
}

// 4. post_response() Implementation (Internal completion of ULID, timestamp, etc.)
func postResponse(nc *nats.Conn, threadID, status, message string) {
	// Automatically append stderr/logs if status == ERROR in a real agent.
	
	payload, _ := json.Marshal(models.ResultPayload{
		OutputDir: fmt.Sprintf("tasks/%s/output/", threadID),
		ExitCode:  0,
		Message:   message,
	})

	// Wrap with strict Schema based on Spec V10
	env := models.MessageEnvelope{
		Type:      "result",
		ThreadID:  &threadID, // Must be ULID (validated by server)
		From:      agentID,
		Timestamp: time.Now().Unix(),
		Payload:   payload,
	}

	data, _ := json.Marshal(env)
	nc.Publish(fmt.Sprintf("board.result.%s", threadID), data)
	log.Printf("  [Skill] post_response: Published %s to board.result.%s", status, threadID)
}

// 6. Persistence & Q&A: Read from local mount without S3
func handleQuery(nc *nats.Conn, data []byte) {
	// Mock: {"thread_id": "01J...", "query": "What is value at step 50?"}
	log.Println("Received direct Q&A query. Reading from local mount directly...")
	time.Sleep(500 * time.Millisecond) // Fast read from local SSD
	log.Println("Answer generated from local /raw_data/. No S3 download required.")
}

func sendStatus(nc *nats.Conn, state string, progress int) {
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

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
