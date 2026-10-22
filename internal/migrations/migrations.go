package migrations

import (
	"github.com/gofiber/fiber/v3/log"

	"github.com/Darlene-13/multi-channel-alerts/internal/db"
	"github.com/Darlene-13/multi-channel-alerts/internal/models"
)

func Migrate() {
	log.Info("Migrating database")

	err := db.DB.AutoMigrate(
		&models.NotificationLog{},
		&models.ApiKey{},
		&models.NotificationAttempt{},
	)
	if err != nil {
		log.Errorf("Unable to migrate database: %v", err)
		return
	}

	log.Info("Done migrating database")
}
