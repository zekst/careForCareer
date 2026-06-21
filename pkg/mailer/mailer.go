package mailer

import (
	"fmt"
	"net/smtp"
)

type Mailer struct {
	host     string
	port     int
	username string
	password string
	from     string
}

func New(host string, port int, username, password, from string) *Mailer {
	return &Mailer{host: host, port: port, username: username, password: password, from: from}
}

func (m *Mailer) SendPasswordReset(to, resetURL string) error {
	subject := "Reset your CareerGPS password"
	body := fmt.Sprintf(`Hi,

You requested a password reset for your CareerGPS account.

Click the link below to set a new password (expires in 1 hour):

%s

If you didn't request this, you can safely ignore this email.

— The CareerGPS Team
`, resetURL)

	return m.send(to, subject, body)
}

func (m *Mailer) send(to, subject, body string) error {
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		m.from, to, subject, body)

	addr := fmt.Sprintf("%s:%d", m.host, m.port)
	auth := smtp.PlainAuth("", m.username, m.password, m.host)
	return smtp.SendMail(addr, auth, m.from, []string{to}, []byte(msg))
}
