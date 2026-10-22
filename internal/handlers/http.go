package handlers

import (
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/log"
	"gorm.io/gorm"

	"github.com/Darlene-13/multi-channel-alerts/internal/db"
	"github.com/Darlene-13/multi-channel-alerts/internal/models"
	"github.com/Darlene-13/multi-channel-alerts/internal/worker"
)

func errorResponse(c fiber.Ctx, status int, message string) error {
	return c.Status(status).JSON(fiber.Map{"error": message})
}

func PingHandler(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"message": "pong"})
}

func CreateAPIKeyHandler(c fiber.Ctx) error {
	payload := new(models.ApiKeyRequest)
	if err := c.Bind().Body(payload); err != nil {
		return errorResponse(c, fiber.StatusBadRequest, "Invalid request payload")
	}

	var existing models.ApiKey
	if err := db.DB.Where("name = ?", payload.Name).First(&existing).Error; err == nil {
		return errorResponse(c, fiber.StatusConflict, "API key with that name already exists")
	}

	key := models.ApiKey{
		Name:   payload.Name,
		Active: true,
	}

	raw, err := key.GenerateToken()
	if err != nil {
		return errorResponse(c, fiber.StatusInternalServerError, "Error generating API Key")
	}

	if err := key.Create(); err != nil {
		return errorResponse(c, fiber.StatusInternalServerError, "Internal server error")
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"uuid": key.Uid,
		"name": key.Name,
		"key":  raw,
		"note": "Store this key now. It cannot be shown again.",
	})
}

// lookupErrorResponse turns a failed key lookup into 404 when the key does
// not exist, and 500 for anything else (e.g. the database is down).
func lookupErrorResponse(c fiber.Ctx, err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errorResponse(c, fiber.StatusNotFound, "API key not found")
	}
	log.Error(err)
	return errorResponse(c, fiber.StatusInternalServerError, "Internal server error")
}

func RevokeAPIKeyHandler(c fiber.Ctx) error {
	key := &models.ApiKey{}
	if err := key.RetrieveByUid(c.Params("id")); err != nil {
		return lookupErrorResponse(c, err)
	}

	if err := key.Revoke(); err != nil {
		return errorResponse(c, fiber.StatusInternalServerError, "Internal server error")
	}

	return c.SendStatus(fiber.StatusOK)
}

func DeleteAPIKeyHandler(c fiber.Ctx) error {
	key := &models.ApiKey{}
	if err := key.RetrieveByUid(c.Params("id")); err != nil {
		return lookupErrorResponse(c, err)
	}

	if err := key.Delete(); err != nil {
		return errorResponse(c, fiber.StatusInternalServerError, "Internal server error")
	}

	return c.SendStatus(fiber.StatusOK)
}

func APIKeysHandler(c fiber.Ctx) error {
	limit, err := strconv.Atoi(c.Params("lim"))
	if err != nil || limit < 0 {
		return c.SendStatus(fiber.StatusBadRequest)
	}

	var keys []models.ApiKey
	if err := db.DB.Order("created_at desc").Limit(limit).Find(&keys).Error; err != nil {
		log.Error(err)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.Status(fiber.StatusOK).JSON(keys)
}

func ViewAPIKeyHandler(c fiber.Ctx) error {
	var key models.ApiKey
	if err := key.RetrieveByUid(c.Params("id")); err != nil {
		return lookupErrorResponse(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(key)
}

func SMSNotificationEnqueueHandler(c fiber.Ctx) error {
	var payload models.SMSNotificationRequest
	if err := c.Bind().Body(&payload); err != nil {
		return errorResponse(c, fiber.StatusBadRequest, "Invalid request payload")
	}

	job := worker.NotificationJob{
		Channel:  worker.SMS_CHANNEL,
		Severity: payload.Severity,
		DestinationInfo: worker.DestinationInfo{
			Recepient: payload.To,
			Body:      payload.Message,
		},
	}

	if err := job.Enqueue(); err != nil {
		log.Errorf("Unable to enqueue SMS job: %v", err)
		return errorResponse(c, fiber.StatusInternalServerError, "Unable to enqueue notification")
	}

	return c.SendStatus(fiber.StatusAccepted)
}

func EmailNotificationEnqueueHandler(c fiber.Ctx) error {
	var payload models.EmailNotificationRequest
	if err := c.Bind().Body(&payload); err != nil {
		return errorResponse(c, fiber.StatusBadRequest, "Invalid request payload")
	}

	job := worker.NotificationJob{
		Channel:  worker.EMAIL_CHANNEL,
		Severity: payload.Severity,
		DestinationInfo: worker.DestinationInfo{
			Recepient: payload.To,
			Subject:   payload.Subject,
			Body:      payload.Body,
		},
	}

	if err := job.Enqueue(); err != nil {
		log.Errorf("Unable to enqueue email job: %v", err)
		return errorResponse(c, fiber.StatusInternalServerError, "Unable to enqueue notification")
	}

	return c.SendStatus(fiber.StatusAccepted)
}
