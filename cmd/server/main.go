package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"metering-api/internal/config"
	"metering-api/internal/database"
	"metering-api/internal/handlers"
	"metering-api/internal/services"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

func main() {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// Connect to database
	db, err := database.Connect(cfg.DBPath)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	// Initialize services
	meteringService, err := services.NewMeteringService(db, cfg.RequestLimit)
	if err != nil {
		log.Fatalf("failed to initialize metering service: %v", err)
	}

	storageService, err := services.NewStorageService(db, cfg.StorageLimit, cfg.UploadDir)
	if err != nil {
		log.Fatalf("failed to initialize storage service: %v", err)
	}

	// Initialize handlers
	meteringHandler := handlers.NewMeteringHandler(meteringService)
	storageHandler := handlers.NewStorageHandler(storageService)

	// Create Fiber app
	app := fiber.New(fiber.Config{
		BodyLimit: int(cfg.StorageLimit),
	})

	// Global middleware
	app.Use(recover.New())
	app.Use(logger.New())

	// API routes — metering is handled inside each handler
	api := app.Group("/api")
	api.Post("/endpoint1", meteringHandler.HandleEndpoint1)
	api.Post("/endpoint2", meteringHandler.HandleEndpoint2)
	api.Get("/metrics", meteringHandler.GetMetrics)

	// Storage routes (metering handled inside handler)
	app.Post("/upload", storageHandler.UploadFile)
	app.Get("/storage", storageHandler.GetStorage)

	// Periodic flush of metering counters to DB
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Duration(cfg.FlushInterval) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := meteringService.Flush(); err != nil {
					log.Printf("failed to flush metering data: %v", err)
				}
			case <-done:
				return
			}
		}
	}()

	// Graceful shutdown
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit

		log.Println("shutting down server...")
		close(done)

		// Flush counters before exiting
		if err := meteringService.Flush(); err != nil {
			log.Printf("failed to flush metering data on shutdown: %v", err)
		}

		if err := app.Shutdown(); err != nil {
			log.Printf("failed to shutdown server: %v", err)
		}
	}()

	// Start server
	addr := fmt.Sprintf(":%s", cfg.AppPort)
	log.Printf("server starting on %s", addr)
	if err := app.Listen(addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
