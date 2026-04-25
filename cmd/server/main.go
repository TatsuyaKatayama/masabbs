package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/TatsuyaKatayama/masabbs/internal/api"
	"github.com/TatsuyaKatayama/masabbs/internal/nats"
	"github.com/TatsuyaKatayama/masabbs/internal/storage"
	"github.com/TatsuyaKatayama/masabbs/internal/worker"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// App holds all application dependencies
type App struct {
	DB      *pgxpool.Pool
	NATS    *nats.Client
	Storage *storage.Client
}

func main() {
	// 1. Configuration
	port := getEnv("PORT", "8080")
	dbURL := getEnv("DATABASE_URL", "postgres://user:password@localhost:5432/masabbs?sslmode=disable")
	natsURL := getEnv("NATS_URL", "nats://localhost:4222")
	minioEndpoint := getEnv("MINIO_ENDPOINT", "localhost:9000")
	minioAccessKey := getEnv("MINIO_ACCESS_KEY", "admin")
	minioSecretKey := getEnv("MINIO_SECRET_KEY", "password123")
	minioBucket := getEnv("MINIO_BUCKET", "ma-system")

	// 2. Initialize Dependencies
	ctx := context.Background()

	dbPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
	}
	defer dbPool.Close()

	natsClient, err := nats.Connect(nats.Config{URL: natsURL})
	if err != nil {
		log.Fatalf("Unable to connect to NATS: %v\n", err)
	}
	defer natsClient.Close()

	storageClient, err := storage.NewClient(storage.Config{
		Endpoint:        minioEndpoint,
		AccessKeyID:     minioAccessKey,
		SecretAccessKey: minioSecretKey,
		UseSSL:          false,
		Bucket:          minioBucket,
	})
	if err != nil {
		log.Fatalf("Unable to connect to MinIO: %v\n", err)
	}

	app := &App{
		DB:      dbPool,
		NATS:    natsClient,
		Storage: storageClient,
	}

	// Start Archiver in the background
	archiver := &worker.Archiver{
		DB: app.DB,
		JS: app.NATS.JS,
	}
	if err := archiver.Start(ctx); err != nil {
		log.Fatalf("Unable to start archiver: %v\n", err)
	}

	// Initialize and start WebSocket Hub
	hub := api.NewHub(app.NATS.NC)
	go hub.Run(ctx)

	// 3. Initialize Echo Framework
	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	
	// TODO: Restrict AllowOrigins before production
	e.Use(middleware.CORS())

	// Health check (Checks DB availability)
	e.GET("/health", func(c echo.Context) error {
		if err := app.DB.Ping(c.Request().Context()); err != nil {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{"status": "db error"})
		}
		// NATS and MinIO checks could be added here
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// Register actual API routes
	api.RegisterRoutes(e, app.DB, app.NATS, app.Storage, hub)

	// 4. Start Server
	go func() {
		if err := e.Start(":" + port); err != nil && err != http.ErrServerClosed {
			e.Logger.Fatal("shuting down the server")
		}
	}()

	// 5. Graceful Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	if err := e.Shutdown(shutdownCtx); err != nil {
		e.Logger.Fatal(err)
	}
	log.Println("Server gracefully stopped.")
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
