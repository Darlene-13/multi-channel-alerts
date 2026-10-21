package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3/log"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/Darlene-13/multi-channel-alerts/internal/cache"
	"github.com/Darlene-13/multi-channel-alerts/internal/mailer"
	"github.com/Darlene-13/multi-channel-alerts/internal/models"
	"github.com/Darlene-13/multi-channel-alerts/internal/sms"
)

const (
	messageStream = "notification_jobs"
	retryQueue    = "notification_retry_jobs"

	retryPollInterval = 5 * time.Second
	retryBaseDelay    = 30 * time.Second
	retryMaxDelay     = 15 * time.Minute

	EMAIL_CHANNEL = 0
	SMS_CHANNEL   = 1

	SEVERITY_LOW       = 0
	SEVERITY_HIGH      = 1
	SEVERITY_CRITICAL  = 2
	SEVERITY_EMERGENCY = 3
)

type DestinationInfo struct {
	Subject   string `json:"subject"`
	Recepient string `json:"recepient"`
	Body      string `json:"body"`
}

type NotificationJob struct {
	Uid     uuid.UUID
	LogUid  uuid.UUID
	Channel int
	DestinationInfo
	CreatedAt int64
	Severity  int
}

func (n *NotificationJob) ToDurableLog() *models.NotificationLog {

	return &models.NotificationLog{
		Uid:         n.LogUid,
		JobUid:      n.Uid,
		Channel:     n.Channel,
		Destination: n.DestinationInfo.Recepient,
		Subject:     n.DestinationInfo.Subject,
		Message:     n.DestinationInfo.Body,
		Status:      models.STATUS_ENQUEUED,
		Severity:    n.Severity,
		Deadline:    int(n.CreatedAt + int64(severityDeadline(n.Severity).Seconds())),
		InDLQ:       false,
	}
}

func (n *NotificationJob) Enqueue() error {
	n.Uid = uuid.New()
	n.LogUid = uuid.New()
	n.CreatedAt = time.Now().Unix()

	if err := n.ToDurableLog().Create(); err != nil {
		return fmt.Errorf("unable to create durable log for job: %w", err)
	}

	nJson, err := json.Marshal(n)
	if err != nil {
		return err
	}

	return cache.XAdd(messageStream, string(nJson))
}

func (n *NotificationJob) ScheduleRetry(at time.Time) error {
	nJson, err := json.Marshal(n)
	if err != nil {
		return err
	}

	return cache.ZAdd(retryQueue, string(nJson), float64(at.Unix()))
}

func retryBackoff(retriesLeft int) time.Duration {
	attempt := max(models.MAX_RETRIES-retriesLeft, 1)

	d := retryBaseDelay << (attempt - 1)
	if d <= 0 || d > retryMaxDelay {
		return retryMaxDelay
	}
	return d
}

func severityDeadline(severity int) time.Duration {
	switch severity {
	case SEVERITY_EMERGENCY:
		return time.Second * 10
	case SEVERITY_CRITICAL:
		return time.Minute * 1
	case SEVERITY_HIGH:
		return time.Minute * 5
	default:
		return time.Hour * 1
	}
}

func handleSendFailure(ctx context.Context, msgID string, sendErr error, n NotificationJob, notifLog *models.NotificationLog, errchan chan<- []error, logchan chan<- string) {
	notifLog.LastError = sendErr.Error()
	cache.GetRedisClient().XDel(ctx, messageStream, msgID)

	if notifLog.RetriesLeft <= 0 {
		errchan <- []error{fmt.Errorf("job %s failed after exhausting retries", msgID)}
		notifLog.Status = models.STATUS_FAILED
		notifLog.InDLQ = true
		return
	}

	backoff := retryBackoff(notifLog.RetriesLeft)
	notifLog.Status = models.STATUS_RETRYING
	if err := n.ScheduleRetry(time.Now().Add(backoff)); err != nil {
		errchan <- []error{fmt.Errorf("job %s: failed to schedule retry: %w", msgID, err)}
		return
	}
	logchan <- fmt.Sprintf("Job %s scheduled for retry in %s (%d retries left)", msgID, backoff, notifLog.RetriesLeft)
}

func HandleMessageStream(ctx context.Context, logchan chan<- string, errchan chan<- []error) {
	log.Info("Starting message stream handler...")
	client := cache.GetRedisClient()
	lastId := "$"

	for {
		msgs, err := client.XRead(ctx, &redis.XReadArgs{
			Streams: []string{messageStream, lastId},
			Count:   1,
			Block:   0,
		}).Result()
		if err != nil {
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				return
			}
			errchan <- []error{err}
			time.Sleep(time.Second)
			continue
		}

		for _, msg := range msgs {
			for _, id := range msg.Messages {
				lastId = id.ID

				rawPayload, ok := id.Values["value"].(string)
				if !ok {
					errchan <- []error{fmt.Errorf("job %s: payload is not a string", id.ID)}
					continue
				}

				var payload NotificationJob
				if err := json.Unmarshal([]byte(rawPayload), &payload); err != nil {
					errchan <- []error{fmt.Errorf("job %s: invalid payload: %w", id.ID, err)}
					continue
				}
				logchan <- fmt.Sprintf("Job %s received message %s", id.ID, rawPayload)

				switch payload.Channel {
				case EMAIL_CHANNEL:
					EmailSendWorker(ctx, id.ID, errchan, logchan, payload)
				case SMS_CHANNEL:
					SmsSendWorker(ctx, id.ID, errchan, logchan, payload)
				default:
					log.Warnf("Job %s has unknown channel %d, skipping", id.ID, payload.Channel)
				}
			}
		}
	}
}

func beginProcessing(n NotificationJob) (*models.NotificationLog, error) {
	notifLog := &models.NotificationLog{Uid: n.LogUid}

	if err := notifLog.Retrieve(true); err != nil {
		return nil, fmt.Errorf("unable to retrieve notification log: %w", err)
	}

	notifLog.Status = models.STATUS_PROCESSING
	notifLog.RetriesLeft--
	if err := notifLog.Update(); err != nil {
		return nil, fmt.Errorf("unable to update notification log: %w", err)
	}

	return notifLog, nil
}

func finishProcessing(ctx context.Context, msgID string, notifLog *models.NotificationLog, errchan chan<- []error) {
	notifLog.Status = models.STATUS_COMPLETED
	notifLog.IsCompleted = true
	notifLog.CompletedAt = time.Now().UTC()
	notifLog.LastError = ""
	cache.GetRedisClient().XDel(ctx, messageStream, msgID)

	if err := notifLog.Update(); err != nil {
		errchan <- []error{err}
	}
}

func SmsSendWorker(ctx context.Context, msgID string, errchan chan<- []error, logchan chan<- string, n NotificationJob) {
	log.Infof("Processing SMS Job: %s", msgID)
	notifLog, err := beginProcessing(n)
	if err != nil {
		errchan <- []error{fmt.Errorf("SMS job %s: %w", msgID, err)}
		cache.GetRedisClient().XDel(ctx, messageStream, msgID)
		return
	}

	smsPayload := sms.SMSPayload{
		To:      n.DestinationInfo.Recepient,
		Message: n.DestinationInfo.Body,
	}

	res, err := smsPayload.Send()
	if err != nil {
		errchan <- []error{err}
		handleSendFailure(ctx, msgID, err, n, notifLog, errchan, logchan)
		if err := notifLog.Update(); err != nil {
			errchan <- []error{err}
		}
		return
	}

	logchan <- fmt.Sprintf("SMS Job %s sent successfully: %v", msgID, res)
	finishProcessing(ctx, msgID, notifLog, errchan)
}

func EmailSendWorker(ctx context.Context, msgID string, errchan chan<- []error, logchan chan<- string, n NotificationJob) {
	log.Infof("Processing Email Job: %s", msgID)
	notifLog, err := beginProcessing(n)

	if err != nil {
		errchan <- []error{fmt.Errorf("Email job %s: %w", msgID, err)}
		cache.GetRedisClient().XDel(ctx, messageStream, msgID)
		return
	}
	email := mailer.Email{
		Recepients: []string{n.DestinationInfo.Recepient},
		Subject:    n.DestinationInfo.Subject,
		Body:       n.DestinationInfo.Body,
		Mu:         &sync.Mutex{},
	}

	if err := <-email.SendAsync(ctx); err != nil {
		errchan <- []error{err}
		handleSendFailure(ctx, msgID, err, n, notifLog, errchan, logchan)
		if err := notifLog.Update(); err != nil {
			errchan <- []error{err}
		}
		return
	}

	logchan <- fmt.Sprintf("Email Job %s sent successfully", msgID)
	finishProcessing(ctx, msgID, notifLog, errchan)
}

func HandleRetryQueue(ctx context.Context, logchan chan<- string, errchan chan<- []error) {
	log.Info("Starting retry queue handler...")
	ticker := time.NewTicker(retryPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			due, err := cache.ZRangeByScoreDue(retryQueue, float64(time.Now().Unix()))
			if err != nil {
				errchan <- []error{err}
				continue
			}

			for _, raw := range due {
				if err := cache.XAdd(messageStream, raw); err != nil {
					errchan <- []error{err}
					continue
				}
				if err := cache.ZRem(retryQueue, raw); err != nil {
					errchan <- []error{err}
				}
				logchan <- fmt.Sprintf("Re-enqueued retry job onto %s", messageStream)
			}
		}
	}
}
