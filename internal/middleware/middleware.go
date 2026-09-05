package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/Darlene-13/multi-channel-alerts/internal/models"
)

func Auth() fiber.Handler {
	return func(c fiber.Ctx) error {
		unauthorized := func() error {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
		}

		header := c.Get("Authorization")
		token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		if header == "" || token == "" {
			return unauthorized()
		}

		key := &models.ApiKey{Key: token}
		if err := key.Retrieve(); err != nil || !key.Active {
			return unauthorized()
		}

		c.Locals("key", key)
		return c.Next()
	}
}
