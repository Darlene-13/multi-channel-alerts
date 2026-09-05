package db

import (
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3/log"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/Darlene-13/multi-channel-alerts/internal/env"
)

const defaultSQLitePath = "multi-channel-alerts.sqlite"

var DB *gorm.DB

func Connect() {
	if env.IsProduction() {
		connectPostgres()
		return
	}
	connectSQLite()
}

func connectPostgres() {
	dsn, err := env.GetDatabaseDSN()
	if err != nil {
		log.Fatalf("db: %v", err)
	}

	DB, err = gorm.Open(postgres.New(postgres.Config{DSN: dsn}), &gorm.Config{})
	if err != nil {
		log.Fatalf("db: unable to connect to Postgres: %v", err)
	}
	log.Info("db: connected to Postgres")
}

func connectSQLite() {
	path := env.Get("DB_SQLITE_PATH")
	if path == "" {
		path = defaultSQLitePath
	}

	var err error
	DB, err = gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		log.Fatalf("db: unable to open SQLite database %s: %v", path, err)
	}
	log.Infof("db: using SQLite database %s (%s environment)", path, env.GetWorkingEnvironment())
}
