package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/log"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/idempotency"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"

	"github.com/Darlene-13/multi-channel-alerts/internal/db"
	"github.com/Darlene-13/multi-channel-alerts/internal/env"
	"github.com/Darlene-13/multi-channel-alerts/internal/migrations"
	"github.com/Darlene-13/multi-channel-alerts/internal/router"
	"github.com/Darlene-13/multi-channel-alerts/internal/worker"
)

func securityHeaders(c fiber.Ctx) error {
	c.Set("X-Content-Type-Options", "nosniff")
	c.Set("X-Frame-Options", "DENY")
	c.Set("X-XSS-Protection", "1; mode=block")
	c.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	c.Set("Cache-Control", "no-store, no-cache, must-revalidate")
	c.Set("Pragma", "no-cache")
	return c.Next()
}

func runWorkers(ctx context.Context) {
	log.Info("Starting workers...")

	errChan := make(chan []error, 16)
	logChan := make(chan string, 16)

	go worker.HandleMessageStream(ctx, logChan, errChan)
	go worker.HandleRetryQueue(ctx, logChan, errChan)

	for {
		select {
		case <-ctx.Done():
			return
		case errs := <-errChan:
			for _, err := range errs {
				log.Errorf("Error processing messages: %v", err)
			}
		case msg := <-logChan:
			log.Info(msg)
		}
	}
}

func main() {
	env.Load()
	db.Connect()
	migrations.Migrate()

	app := fiber.New(fiber.Config{
		AppName: "Notifications API",
	})

	app.Use(logger.New())
	app.Use(recover.New())
	app.Use(idempotency.New())
	app.Use(securityHeaders)
	app.Use(cors.New(cors.Config{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "POST", "HEAD", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowHeaders: []string{"*"},
	}))

	router.Begin(app)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if !fiber.IsChild() {
		go runWorkers(ctx)
	}

	addr := env.Get("HTTP_LISTEN_PORT")
	if addr == "" {
		addr = ":8080"
	}

	go func() {
		<-ctx.Done()
		log.Info("Shutting down HTTP server...")
		if err := app.Shutdown(); err != nil {
			log.Errorf("Error during shutdown: %v", err)
		}
	}()

	if err := app.Listen(addr); err != nil {
		log.Errorf("Unable to listen on %s: %v", addr, err)
	}
}
