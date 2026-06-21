package email

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
)

// Sender sends transactional email over SMTP.
type Sender struct {
	host     string
	port     string
	user     string
	password string
	from     string
}

func NewSender(host, port, user, password, from string) *Sender {
	return &Sender{host: host, port: port, user: user, password: password, from: from}
}

// Send delivers a plain-text email. Port 465 uses implicit TLS; all others use STARTTLS via smtp.SendMail.
func (s *Sender) Send(to, subject, body string) error {
	msg := []byte("From: " + s.from + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"\r\n" +
		body)

	addr := net.JoinHostPort(s.host, s.port)
	auth := smtp.PlainAuth("", s.user, s.password, s.host)

	if s.port == "465" {
		return s.sendImplicitTLS(addr, auth, to, msg)
	}
	return smtp.SendMail(addr, auth, s.from, []string{to}, msg)
}

func (s *Sender) sendImplicitTLS(addr string, auth smtp.Auth, to string, msg []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: s.host})
	if err != nil {
		return fmt.Errorf("tls dial: %w", err)
	}
	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := client.Mail(s.from); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	defer wc.Close()
	_, err = wc.Write(msg)
	return err
}
