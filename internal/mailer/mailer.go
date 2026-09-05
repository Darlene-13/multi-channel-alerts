package mailer

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"html/template"
	"io"
	"mime"
	"net"
	"net/smtp"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3/log"

	"github.com/Darlene-13/multi-channel-alerts/internal/env"
)

const (
	defaultSMTPHost = "smtp.gmail.com"
	defaultSMTPPort = 587
)

var SenderEmail, AppPassword string

func init() {

	var err error
	SenderEmail, err = env.Lookup("EMAIL_FROM")
	if err != nil {
		log.Errorf("mail: unable to load sender email from environment: %v", err)
	} else {
		log.Infof("mail: sender email loaded: %s", SenderEmail)
	}

	AppPassword, err = env.Lookup("EMAIL_APP_PASSWORD")
	if err != nil {
		log.Errorf("mail: unable to load app password from environment: %v", err)
	} else {
		log.Info("mail: app password loaded")
	}
}

type EmailCC struct {
	Email string
	Name  string
}

type Email struct {
	Recepients []string
	Body       string
	Subject    string
	EmailCCs   []EmailCC
	Mu         *sync.Mutex
}

func smtpHost() string {
	if host := strings.TrimSpace(os.Getenv("EMAIL_SMTP_HOST")); host != "" {
		return host
	}
	return defaultSMTPHost
}

func smtpPort() int {
	portStr := strings.TrimSpace(os.Getenv("EMAIL_SMTP_PORT"))
	if portStr == "" {
		return defaultSMTPPort
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		return defaultSMTPPort
	}
	return port
}

func smtpAddress() string {
	return fmt.Sprintf("%s:%d", smtpHost(), smtpPort())
}

func smtpUseSSL() bool {
	if value := strings.TrimSpace(os.Getenv("EMAIL_SMTP_SSL")); value != "" {
		return value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes")
	}

	return smtpPort() == 465
}

func smtpHelloName() string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "localhost"
	}
	return name
}

func templatePath(name string) string {
	relative := filepath.Join("internal", "templates", "emails", name)
	if _, err := os.Stat(relative); err == nil {
		return relative
	}

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return relative
	}

	projectRoot := filepath.Dir(filepath.Dir(filepath.Dir(currentFile)))
	return filepath.Join(projectRoot, "internal", "templates", "emails", name)
}

func renderEmailTemplate(name string, data any) (string, error) {
	tmpl, err := template.ParseFiles(templatePath(name))
	if err != nil {
		return "", err
	}

	var body bytes.Buffer
	if err := tmpl.Execute(&body, data); err != nil {
		return "", err
	}

	return body.String(), nil
}

func (e *Email) recipients() []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(e.Recepients)+len(e.EmailCCs))

	add := func(addr string) {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			return
		}
		if _, ok := seen[addr]; ok {
			return
		}
		seen[addr] = struct{}{}
		result = append(result, addr)
	}

	for _, addr := range e.Recepients {
		add(addr)
	}
	for _, cc := range e.EmailCCs {
		add(cc.Email)
	}

	return result
}

func (e *Email) buildMessage() ([]byte, error) {
	recipients := e.recipients()
	if len(recipients) == 0 {
		return nil, fmt.Errorf("no recipients provided")
	}

	var buf bytes.Buffer

	fmt.Fprintf(&buf, "From: %s\r\n", "Darlene")
	fmt.Fprintf(&buf, "To: %s\r\n", strings.Join(e.Recepients, ", "))
	if len(e.EmailCCs) > 0 {
		ccNames := make([]string, 0, len(e.EmailCCs))
		for _, cc := range e.EmailCCs {
			if cc.Name != "" {
				ccNames = append(ccNames, fmt.Sprintf("%s <%s>", cc.Name, cc.Email))
				continue
			}
			ccNames = append(ccNames, cc.Email)
		}
		fmt.Fprintf(&buf, "Cc: %s\r\n", strings.Join(ccNames, ", "))
	}
	fmt.Fprintf(&buf, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", e.Subject))
	fmt.Fprintf(&buf, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Content-Type: text/html; charset=\"UTF-8\"\r\n")
	fmt.Fprintf(&buf, "\r\n")
	fmt.Fprint(&buf, e.Body)

	return buf.Bytes(), nil
}

func (e *Email) sendWithContext(ctx context.Context) error {
	start := time.Now()
	mu := e.Mu
	if mu == nil {
		mu = &sync.Mutex{}
	}

	log.Infof("mail: send begin subject=%q to=%v cc=%d", e.Subject, e.Recepients, len(e.EmailCCs))

	mu.Lock()
	defer mu.Unlock()
	log.Debugf("mail: acquired send lock (waited %s)", time.Since(start))

	if strings.TrimSpace(SenderEmail) == "" {
		log.Errorf("mail: SenderEmail is empty — check EMAIL_FROM env var")
		return fmt.Errorf("sender email not configured")
	}
	if strings.TrimSpace(AppPassword) == "" {
		log.Warnf("mail: AppPassword is empty — AUTH will be skipped, most relays will reject the message")
	}

	message, buildErr := e.buildMessage()
	if buildErr != nil {
		log.Errorf("mail: build message failed: %v", buildErr)
		return fmt.Errorf("failed to build message: %w", buildErr)
	}
	log.Debugf("mail: built message (%d bytes)", len(message))

	recipients := e.recipients()
	addr := smtpAddress()
	host := smtpHost()
	useSSL := smtpUseSSL()
	log.Infof("mail: dialing smtp host=%s addr=%s ssl=%t recipients=%v", host, addr, useSSL, recipients)

	var conn net.Conn
	var dialErr error
	dialStart := time.Now()
	if useSSL {
		conn, dialErr = tls.DialWithDialer(&net.Dialer{Timeout: 15 * time.Second}, "tcp", addr, &tls.Config{ServerName: host})
	} else {
		dialer := &net.Dialer{Timeout: 15 * time.Second}
		conn, dialErr = dialer.DialContext(ctx, "tcp", addr)
	}
	if dialErr != nil {
		log.Errorf("mail: dial %s failed after %s: %v", addr, time.Since(dialStart), dialErr)
		return fmt.Errorf("failed to dial smtp server %s: %w", addr, dialErr)
	}
	log.Debugf("mail: dial ok in %s", time.Since(dialStart))
	defer conn.Close()

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		log.Errorf("mail: new smtp client failed: %v", err)
		return fmt.Errorf("failed to create smtp client: %w", err)
	}
	defer client.Close()

	helloName := smtpHelloName()
	if err := client.Hello(helloName); err != nil {
		log.Errorf("mail: EHLO/HELO %s failed: %v", helloName, err)
		return fmt.Errorf("smtp hello failed: %w", err)
	}
	log.Debugf("mail: EHLO %s ok", helloName)

	if !useSSL {
		if ok, _ := client.Extension("STARTTLS"); ok {
			log.Debugf("mail: server supports STARTTLS, upgrading")
			if err := client.StartTLS(&tls.Config{ServerName: host}); err != nil {
				log.Errorf("mail: STARTTLS failed: %v", err)
				return fmt.Errorf("smtp starttls failed: %w", err)
			}
			log.Debugf("mail: STARTTLS upgrade ok")
		} else {
			log.Warnf("mail: server does not advertise STARTTLS, continuing in plaintext")
		}
	}

	if AppPassword != "" {
		auth := smtp.PlainAuth("", SenderEmail, AppPassword, host)
		if ok, _ := client.Extension("AUTH"); ok {
			log.Debugf("mail: attempting AUTH PLAIN as %s", SenderEmail)
			if err := client.Auth(auth); err != nil {
				log.Errorf("mail: AUTH failed for %s: %v", SenderEmail, err)
				return fmt.Errorf("smtp auth failed: %w", err)
			}
			log.Infof("mail: AUTH ok as %s", SenderEmail)
		} else {
			log.Warnf("mail: server does not advertise AUTH extension — message will be sent unauthenticated")
		}
	} else {
		log.Warnf("mail: skipping AUTH because AppPassword is empty")
	}

	if err := client.Mail(SenderEmail); err != nil {
		log.Errorf("mail: MAIL FROM <%s> failed: %v", SenderEmail, err)
		return fmt.Errorf("smtp mail from failed: %w", err)
	}
	log.Debugf("mail: MAIL FROM <%s> ok", SenderEmail)
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			log.Errorf("mail: RCPT TO <%s> failed: %v", recipient, err)
			return fmt.Errorf("smtp rcpt to %s failed: %w", recipient, err)
		}
		log.Debugf("mail: RCPT TO <%s> ok", recipient)
	}

	wc, err := client.Data()
	if err != nil {
		log.Errorf("mail: DATA command failed: %v", err)
		return fmt.Errorf("smtp data failed: %w", err)
	}
	n, err := io.Copy(wc, bytes.NewReader(message))
	if err != nil {
		_ = wc.Close()
		log.Errorf("mail: writing body failed after %d bytes: %v", n, err)
		return fmt.Errorf("smtp write body failed: %w", err)
	}
	if err := wc.Close(); err != nil {
		log.Errorf("mail: closing DATA writer failed: %v", err)
		return fmt.Errorf("smtp close body failed: %w", err)
	}
	log.Debugf("mail: DATA wrote %d bytes ok", n)

	if err := client.Quit(); err != nil {
		log.Errorf("mail: QUIT failed: %v", err)
		return fmt.Errorf("smtp quit failed: %w", err)
	}

	log.Infof("mail: send ok subject=%q recipients=%d duration=%s", e.Subject, len(recipients), time.Since(start))
	return nil
}

func (e *Email) Send(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return e.sendWithContext(ctx)
}

func (e *Email) SendAsync(ctx context.Context) <-chan error {
	errCh := make(chan error, 1)
	log.Debugf("mail: SendAsync dispatched subject=%q", e.Subject)
	go func() {
		err := e.Send(ctx)
		if err != nil {
			log.Errorf("mail: SendAsync result subject=%q err=%v", e.Subject, err)
		} else {
			log.Infof("mail: SendAsync result subject=%q ok", e.Subject)
		}
		errCh <- err
	}()
	return errCh
}
