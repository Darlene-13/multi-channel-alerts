package env

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/gofiber/fiber/v3/log"
	"github.com/joho/godotenv"
)

var loadOnce sync.Once

func init() { Load() }

func Load() {
	loadOnce.Do(func() {
		cwd, err := os.Getwd()
		if err != nil {
			log.Errorf("env: unable to determine working directory: %v", err)
			return
		}

		path := filepath.Join(cwd, ".env")
		if err := godotenv.Load(path); err != nil {
			log.Warnf("env: unable to load %s: %v (falling back to process environment)", path, err)
			return
		}
		log.Infof("env: loaded %s", path)
	})
}

func Get(key string) string {
	return os.Getenv(key)
}

func Lookup(key string) (string, error) {
	value := os.Getenv(key)
	if value == "" {
		return "", fmt.Errorf("env: %s is not set", key)
	}
	return value, nil
}

func GetInt(key string) int {
	value := os.Getenv(key)
	if value == "" {
		return 0
	}

	n, err := strconv.Atoi(value)
	if err != nil {
		log.Warnf("env: %s=%q is not an integer, using 0", key, value)
		return 0
	}
	return n
}

const (
	PRODUCTION  = "production"
	DEVELOPMENT = "development"
)

func GetWorkingEnvironment() string {
	switch value := os.Getenv("APP_ENV"); value {
	case PRODUCTION, "prod":
		return PRODUCTION
	case "":
		return DEVELOPMENT
	default:
		return value
	}
}

func IsProduction() bool {
	return GetWorkingEnvironment() == PRODUCTION
}

func GetDatabaseDSN() (string, error) {
	var missing []string
	for _, key := range []string{"DB_HOST", "DB_USER", "DB_NAME", "DB_PORT"} {
		if os.Getenv(key) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("env: missing required database variables: %v", missing)
	}

	tz := os.Getenv("DB_TIMEZONE")
	if tz == "" {
		tz = "UTC"
	}

	return fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%d sslmode=disable TimeZone=%s",
		os.Getenv("DB_HOST"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"),
		GetInt("DB_PORT"),
		tz,
	), nil
}
