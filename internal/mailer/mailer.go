package mailer

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net/smtp"
	"os"
	"strings"

	"github.com/ivantit66/onebase/internal/dsl/interpreter"
)

// Config holds SMTP settings from config/app.yaml section "email".
type Config struct {
	SMTPHost    string `yaml:"smtp_host"`
	SMTPPort    int    `yaml:"smtp_port"`
	SMTPUser    string `yaml:"smtp_user"`
	SMTPPass    string `yaml:"smtp_password"` // or "env:VAR_NAME"
	FromName    string `yaml:"from_name"`
	FromAddress string `yaml:"from_address"`
}

type Mailer struct {
	cfg Config
}

func New(cfg Config) *Mailer {
	return &Mailer{cfg: cfg}
}

func (m *Mailer) Configured() bool {
	return m != nil && m.cfg.SMTPHost != ""
}

// Send delivers an email. Pass empty htmlBody for plain-text only.
func (m *Mailer) Send(to, subject, textBody, htmlBody string) error {
	return m.SendWithAttachments(to, subject, textBody, htmlBody, nil)
}

// SendWithAttachments delivers an email with file attachments (multipart/mixed).
// Реализует interpreter.EmailAttachmentSender — DSL-объект ПисьмоEmail
// использует его при наличии вложений (ПисьмоEmail.ПрисоединитьФайл).
func (m *Mailer) SendWithAttachments(to, subject, textBody, htmlBody string, files []interpreter.EmailAttachment) error {
	if !m.Configured() {
		return fmt.Errorf("email не настроен — добавьте секцию email в config/app.yaml")
	}
	port := m.cfg.SMTPPort
	if port == 0 {
		port = 587
	}
	addr := fmt.Sprintf("%s:%d", m.cfg.SMTPHost, port)

	from := m.cfg.FromAddress
	if m.cfg.FromName != "" {
		from = fmt.Sprintf("%s <%s>", m.cfg.FromName, m.cfg.FromAddress)
	}

	msg := buildMsgWithFiles(from, to, subject, textBody, htmlBody, files)

	var auth smtp.Auth
	if m.cfg.SMTPUser != "" {
		auth = smtp.PlainAuth("", m.cfg.SMTPUser, m.password(), m.cfg.SMTPHost)
	}

	if port == 465 {
		return sendTLS(addr, m.cfg.SMTPHost, auth, m.cfg.FromAddress, to, msg)
	}
	return smtp.SendMail(addr, auth, m.cfg.FromAddress, []string{to}, msg)
}

func (m *Mailer) password() string {
	if strings.HasPrefix(m.cfg.SMTPPass, "env:") {
		return os.Getenv(strings.TrimPrefix(m.cfg.SMTPPass, "env:"))
	}
	return m.cfg.SMTPPass
}

func sendTLS(addr, host string, auth smtp.Auth, from, to string, msg []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Quit() //nolint:errcheck
	if auth != nil {
		if err = c.Auth(auth); err != nil {
			return err
		}
	}
	if err = c.Mail(from); err != nil {
		return err
	}
	if err = c.Rcpt(to); err != nil {
		return err
	}
	wc, err := c.Data()
	if err != nil {
		return err
	}
	defer wc.Close() //nolint:errcheck
	_, err = wc.Write(msg)
	return err
}

func buildMsg(from, to, subject, textBody, htmlBody string) []byte {
	return buildMsgWithFiles(from, to, subject, textBody, htmlBody, nil)
}

func buildMsgWithFiles(from, to, subject, textBody, htmlBody string, files []interpreter.EmailAttachment) []byte {
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")

	if len(files) > 0 {
		const mixed = "==boundary_onebase_mixed"
		b.WriteString("Content-Type: multipart/mixed; boundary=\"" + mixed + "\"\r\n\r\n")
		// Тело письма — вложенной частью (alternative при наличии HTML).
		b.WriteString("--" + mixed + "\r\n")
		writeBodyPart(&b, textBody, htmlBody)
		// Файлы — base64 с переносом строк по RFC 2045.
		for _, f := range files {
			mt := f.MimeType
			if mt == "" {
				mt = "application/octet-stream"
			}
			b.WriteString("--" + mixed + "\r\n")
			b.WriteString("Content-Type: " + mt + "; name=\"" + sanitizeHeaderValue(f.Name) + "\"\r\n")
			b.WriteString("Content-Disposition: attachment; filename=\"" + sanitizeHeaderValue(f.Name) + "\"\r\n")
			b.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
			writeBase64Wrapped(&b, f.Data)
			b.WriteString("\r\n")
		}
		b.WriteString("--" + mixed + "--\r\n")
		return []byte(b.String())
	}

	writeBodyPart(&b, textBody, htmlBody)
	return []byte(b.String())
}

// writeBodyPart пишет заголовок Content-Type и тело (text и/или html).
// Вызывается и как корень письма без вложений, и как часть multipart/mixed.
func writeBodyPart(b *strings.Builder, textBody, htmlBody string) {
	if htmlBody != "" {
		const boundary = "==boundary_onebase_email"
		b.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n\r\n")
		if textBody != "" {
			b.WriteString("--" + boundary + "\r\n")
			b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
			b.WriteString(textBody + "\r\n")
		}
		b.WriteString("--" + boundary + "\r\n")
		b.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
		b.WriteString(htmlBody + "\r\n")
		b.WriteString("--" + boundary + "--\r\n")
	} else {
		b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
		b.WriteString(textBody + "\r\n")
	}
}

// sanitizeHeaderValue убирает из имени файла символы, ломающие MIME-заголовок.
func sanitizeHeaderValue(s string) string {
	s = strings.NewReplacer("\r", "", "\n", "", "\"", "'").Replace(s)
	return s
}

// writeBase64Wrapped пишет данные в base64 строками по 76 символов (RFC 2045).
func writeBase64Wrapped(b *strings.Builder, data []byte) {
	enc := base64.StdEncoding.EncodeToString(data)
	const lineLen = 76
	for len(enc) > lineLen {
		b.WriteString(enc[:lineLen] + "\r\n")
		enc = enc[lineLen:]
	}
	if enc != "" {
		b.WriteString(enc + "\r\n")
	}
}
