package sms

import (
	"strings"

	at "github.com/astianmuchui/at-sms-gofiber/v1"
	"github.com/gofiber/fiber/v3/log"

	"github.com/Darlene-13/multi-channel-alerts/internal/env"
)

const SENDER_ID = "Darlene Wendy"

type SMSPayload struct {
	To      string
	Message string
}

func init() {
	at.SetProductionMode()
	at.SetCredentials(env.Get("AFRICAS_TALKING_USERNAME"), env.Get("AFRICAS_TALKING_API_KEY"))
}

func PrependPhonePlus(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "+") {
		return s
	}
	return "+" + s
}

func (s *SMSPayload) Send() (*at.BulkSMSResponsePayload, error) {
	payload := &at.BulkSMSRequestPayload{
		PhoneNumbers: []string{PrependPhonePlus(s.To)},
		Message:      s.Message,
		SenderId:     SENDER_ID,
		Username:     env.Get("AFRICAS_TALKING_USERNAME"),
		Enqueue:      true,
	}

	res, err := payload.Send()
	if err != nil {
		log.Errorf("sms: send to %s failed: %v", s.To, err)
		return &at.BulkSMSResponsePayload{}, err
	}

	return res, nil
}
