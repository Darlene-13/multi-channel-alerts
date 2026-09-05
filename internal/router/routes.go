package router

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cache"

	"github.com/Darlene-13/multi-channel-alerts/internal/handlers"
	"github.com/Darlene-13/multi-channel-alerts/internal/middleware"
)

func Begin(app *fiber.App) {
	cached := cache.New(cache.Config{Expiration: time.Minute})

	auth := app.Group("/notifications/api/v0/auth")
	auth.Post("/api-key", handlers.CreateAPIKeyHandler)
	auth.Patch("/api-key/revoke/:key", handlers.RevokeAPIKeyHandler)
	auth.Delete("/api-key/:key", handlers.DeleteAPIKeyHandler)

	v0 := app.Group("/notifications/api/v0", middleware.Auth())
	v0.Get("/api-keys/:lim", handlers.APIKeysHandler, cached)
	v0.Get("/api-key/view/:id", handlers.ViewAPIKeyHandler, cached)

	v1 := app.Group("/notifications/api/v1")
	v1.Get("/ping", handlers.PingHandler)
	v1.Post("/sms/enqueue", handlers.SMSNotificationEnqueueHandler)
	v1.Post("/email/enqueue", handlers.EmailNotificationEnqueueHandler)
}
