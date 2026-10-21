package models

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3/log"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Darlene-13/multi-channel-alerts/internal/cache"
	"github.com/Darlene-13/multi-channel-alerts/internal/db"
)

const (
	SMS   = "sms"
	EMAIL = "email"

	STATUS_ENQUEUED   = 0
	STATUS_PROCESSING = 1
	STATUS_RETRYING   = 2
	STATUS_FAILED     = 3
	MAX_RETRIES       = 5
	STATUS_COMPLETED  = 4

	ATTEMPT_STATUS          = "success"
	ATTEMPT_FAILURE         = "failure"
	notificationLogCacheTTL = 200
)

type NotificationLog struct {
	gorm.Model

	Uid         uuid.UUID `json:"uid"`
	JobUid      uuid.UUID `json:"job_uid"`
	Channel     int       `json:"channel"`
	Message     string    `json:"message"`
	Subject     string    `json:"subject"`
	Destination string    `json:"destination"`
	Status      int       `json:"status"`

	Severity    int `json:"severity"`
	RetriesLeft int `json:"retries_left"`
	Deadline    int `json:"deadline"`

	CompletedAt time.Time `json:"completed_at"`
	IsCompleted bool      `json:"is_completed"`
	InDLQ       bool      `json:"in_dlq"`

	LastError string `json:"last_error"`
}

type SMSNotificationRequest struct {
	To       string `json:"to"`
	Message  string `json:"message"`
	Severity int    `json:"severity"`
}

type EmailNotificationRequest struct {
	To       string `json:"to"`
	Subject  string `json:"subject"`
	Body     string `json:"body"`
	Severity int    `json:"severity"`
}

func (n *NotificationLog) cacheKey() string {
	return fmt.Sprintf("notification_log_%s", n.Uid.String())
}

func (n *NotificationLog) BeforeCreate(tx *gorm.DB) error {
	n.IsCompleted = false
	n.RetriesLeft = MAX_RETRIES
	return nil
}

func (n *NotificationLog) AfterCreate(tx *gorm.DB) error {
	return n.Encache()
}

func (n *NotificationLog) AfterDelete(tx *gorm.DB) error {
	return n.Decache()
}

func (n *NotificationLog) Create() error {
	return db.DB.Create(n).Error
}

func (n *NotificationLog) Update() error {
	if err := db.DB.Save(n).Error; err != nil {
		return err
	}
	return n.Encache()
}

func (n *NotificationLog) Delete() error {
	return db.DB.Delete(n).Error
}

func (n *NotificationLog) Retrieve(useCache bool) error {
	if useCache {
		if nJson, err := cache.Read(n.cacheKey()); err == nil {
			cached := NotificationLog{}
			if err := json.Unmarshal([]byte(nJson), &cached); err == nil && cached.ID != 0 {
				*n = cached
				return nil
			}
		}
	}

	return db.DB.Where("uid = ?", n.Uid).First(n).Error
}

func (n *NotificationLog) Encache() error {
	nJson, err := json.Marshal(n)
	if err != nil {
		return err
	}
	return cache.Remember(n.cacheKey(), string(nJson), notificationLogCacheTTL)
}

func (n *NotificationLog) Decache() error {
	return cache.Forget(n.cacheKey())
}

type ApiKey struct {
	gorm.Model
	Name   string    `json:"name" gorm:"uniqueIndex:idx_name"`
	Uid    uuid.UUID `json:"uuid" gorm:"uniqueIndex:idx_uuid"`
	Key    string    `json:"key" gorm:"uniqueIndex:idx_key"`
	Active bool      `json:"active"`
}

type ApiKeyRequest struct {
	Name string `json:"name"`
}

func (a *ApiKey) BeforeCreate(tx *gorm.DB) error {
	a.Uid = uuid.New()
	return nil
}

func (a *ApiKey) Create() error {
	return db.DB.Create(a).Error
}

func (a *ApiKey) GenerateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		log.Errorf("Unable to generate token: %v", err)
		return "", err
	}

	a.Key = hex.EncodeToString(buf)
	return a.Key, nil
}

func (a *ApiKey) Retrieve() error {
	return db.DB.Where("key = ?", a.Key).First(a).Error
}

func (a *ApiKey) Update() error {
	return db.DB.Save(a).Error
}

func (a *ApiKey) Revoke() error {
	a.Active = false
	return a.Update()
}

func (a *ApiKey) Activate() error {
	a.Active = true
	return a.Update()
}

func (a *ApiKey) Delete() error {
	return db.DB.Delete(a).Error
}

func GetAllKeys() ([]ApiKey, error) {
	var keys []ApiKey
	return keys, db.DB.Find(&keys).Error
}

func cacheApiKey(rdbKey string, apiKey *ApiKey) {
	apiKeyJson, err := json.Marshal(apiKey)
	if err != nil {
		return
	}
	if err := cache.RememberForever(rdbKey, apiKeyJson); err != nil {
		log.Errorf("Error caching API key in Redis: %v", err)
	}
}

func RetrieveInternalKey() (*ApiKey, error) {
	const rdbKey = "API_KEY"
	var apiKey ApiKey

	if key, err := cache.Read(rdbKey); err == nil && key != "" {
		if err := json.Unmarshal([]byte(key), &apiKey); err != nil {
			log.Errorf("Error unmarshalling API key from Redis: %v", err)
			return nil, err
		}
		return &apiKey, nil
	}

	fetchErr := db.DB.Where("name = ?", rdbKey).First(&apiKey).Error
	if fetchErr == nil {
		cacheApiKey(rdbKey, &apiKey)
		return &apiKey, nil
	}
	if !errors.Is(fetchErr, gorm.ErrRecordNotFound) {
		return nil, fetchErr
	}

	log.Warn("No internal API key found, generating a new one.")
	apiKey.Name = rdbKey
	apiKey.Active = true

	if _, err := apiKey.GenerateToken(); err != nil {
		return nil, err
	}
	if err := apiKey.Create(); err != nil {
		log.Errorf("Error creating API key in database: %v", err)
		return nil, err
	}

	cacheApiKey(rdbKey, &apiKey)
	return &apiKey, nil
}

type NotificationAttempt struct {
	gorm.Model

	NotificationUid uuid.UUID `json:"notification_uid"`
	AttemptNumber   int       `json:"attempt-number"`
	Status          string    `json:"status"`
	Error           string    `json:"error"`
}

func (a *NotificationAttempt) create() error {
	return db.DB.Create(a).Error
}
