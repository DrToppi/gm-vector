package planner

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net"
	"net/smtp"
	"net/textproto"
	"os"
	"strings"
	"time"
)

type SMTPConfig struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Security string `json:"security"`
	From     string `json:"from"`
	Username string `json:"username"`
	Password string `json:"password"`
	CAFile   string `json:"ca_file,omitempty"`
	roots    *x509.CertPool
}

func validateSMTP(c SMTPConfig) error {
	if !c.Enabled {
		return nil
	}
	if c.Host == "" || len(c.Host) > 253 || strings.ContainsAny(c.Host, " \r\n\t/\\") || c.Port < 1 || c.Port > 65535 || (c.Security != "tls" && c.Security != "starttls") || !validEmail(c.From) {
		return errors.New("invalid SMTP configuration")
	}
	if (c.Username == "") != (c.Password == "") || strings.ContainsAny(c.Username, "\r\n\x00") || strings.ContainsRune(c.Password, 0) {
		return errors.New("invalid SMTP credentials configuration")
	}
	return nil
}
func loadSMTP(path string) (SMTPConfig, error) {
	var c SMTPConfig
	if e := CheckFileMode(path); e != nil {
		return c, e
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return c, errors.New("cannot read SMTP configuration")
	}
	if e = json.Unmarshal(b, &c); e != nil {
		return c, errors.New("invalid SMTP JSON")
	}
	if e = validateSMTP(c); e != nil {
		return c, e
	}
	if c.Enabled && c.CAFile != "" {
		b, e = os.ReadFile(c.CAFile)
		if e != nil {
			return c, errors.New("cannot read SMTP CA")
		}
		c.roots, _ = x509.SystemCertPool()
		if c.roots == nil {
			c.roots = x509.NewCertPool()
		}
		if !c.roots.AppendCertsFromPEM(b) {
			return c, errors.New("invalid SMTP CA")
		}
	}
	return c, nil
}
func mailMessage(c SMTPConfig, m TaskComment) string {
	body := fmt.Sprintf("gmVector\nProject: %s\nTask: %s\nAuthor: %s\n\n%s\n", m.ProjectID, m.TaskID, m.AuthorName, m.Text)
	return smtpMessage(c, m.ID, m.Recipient, "gmVector task comment", body)
}
func smtpMessage(c SMTPConfig, id, recipient, subject, body string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	var lines strings.Builder
	for len(encoded) > 76 {
		lines.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	lines.WriteString(encoded + "\r\n")
	domain := strings.SplitN(c.From, "@", 2)[1]
	return "From: " + c.From + "\r\nTo: " + recipient + "\r\nSubject: " + subject + "\r\nDate: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\nMessage-ID: <" + id + "@" + domain + ">\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n" + lines.String()
}
func sendCustomerMail(ctx context.Context, c SMTPConfig, m TaskComment) (string, string) {
	if !c.Enabled {
		return "failed", "email_disabled"
	}
	if validateSMTP(c) != nil || !validEmail(m.Recipient) {
		return "failed", "invalid_mail_configuration"
	}
	return sendSMTPMessage(ctx, c, m.Recipient, mailMessage(c, m))
}
func sendSMTPMessage(ctx context.Context, c SMTPConfig, recipient, message string) (string, string) {
	if !c.Enabled || validateSMTP(c) != nil || !validEmail(recipient) {
		return "failed", "invalid_mail_configuration"
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	tc := &tls.Config{ServerName: c.Host, RootCAs: c.roots, MinVersion: tls.VersionTLS12}
	address := net.JoinHostPort(c.Host, fmt.Sprint(c.Port))
	var conn net.Conn
	var e error
	if c.Security == "tls" {
		conn, e = (&tls.Dialer{Config: tc}).DialContext(ctx, "tcp", address)
	} else {
		conn, e = (&net.Dialer{}).DialContext(ctx, "tcp", address)
	}
	if e != nil {
		return "failed", "smtp_connection_failed"
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)
	client, e := smtp.NewClient(conn, c.Host)
	if e != nil {
		return "failed", "smtp_greeting_failed"
	}
	defer client.Close()
	if c.Security == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return "failed", "smtp_tls_required"
		}
		if e = client.StartTLS(tc); e != nil {
			return "failed", "smtp_tls_failed"
		}
	}
	state, ok := client.TLSConnectionState()
	if !ok || !state.HandshakeComplete {
		return "failed", "smtp_tls_required"
	}
	if c.Username != "" {
		if e = client.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host)); e != nil {
			return "failed", "smtp_auth_failed"
		}
	}
	if e = client.Mail(c.From); e != nil {
		return "failed", "smtp_sender_rejected"
	}
	if e = client.Rcpt(recipient); e != nil {
		return "failed", "smtp_recipient_rejected"
	}
	writer, e := client.Data()
	if e != nil {
		return "failed", "smtp_data_rejected"
	}
	if _, e = writer.Write([]byte(message)); e != nil {
		return "uncertain", "smtp_confirmation_missing"
	}
	if e = writer.Close(); e != nil {
		var rejection *textproto.Error
		if errors.As(e, &rejection) && rejection.Code >= 400 {
			return "failed", "smtp_message_rejected"
		}
		return "uncertain", "smtp_confirmation_missing"
	}
	client.Quit()
	return "sent", ""
}
func (a *App) customerMailTick(ctx context.Context) error {
	if _, e := a.Store.DB.Exec(ctx, "UPDATE outbox SET state='uncertain',last_error='delivery_interrupted',leased_until=NULL WHERE provider='smtp' AND state='sending' AND leased_until<now()"); e != nil {
		return e
	}
	if !a.Config.SMTP.Enabled {
		return nil
	}
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var id, project string
	e = tx.QueryRow(ctx, "SELECT id,project_id FROM outbox WHERE provider='smtp' AND state='queued' AND ready_at<=now() ORDER BY ready_at,id FOR UPDATE SKIP LOCKED LIMIT 1").Scan(&id, &project)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE outbox SET state='sending',attempts=attempts+1,leased_until=now()+interval '60 seconds',last_error='' WHERE id=$1", id); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	return a.deliverCustomerMail(ctx, id, project)
}
func (a *App) deliverCustomerMail(ctx context.Context, id, project string) error {
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var projectID string
	e = tx.QueryRow(ctx, "SELECT id FROM projects WHERE id=$1 FOR UPDATE", project).Scan(&projectID)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	var state string
	if e = tx.QueryRow(ctx, "SELECT state FROM outbox WHERE id=$1 AND provider='smtp' FOR UPDATE", id).Scan(&state); e != nil {
		return e
	}
	if state != "sending" {
		return nil
	}
	finish := func(state, reason string) error {
		if _, e := tx.Exec(ctx, "UPDATE outbox SET state=$2,last_error=$3,leased_until=NULL WHERE id=$1", id, state, reason); e != nil {
			return e
		}
		return tx.Commit(ctx)
	}
	m, e := readComment(ctx, tx, id)
	if errors.Is(e, pgx.ErrNoRows) {
		return finish("failed", "task_deleted")
	}
	if e != nil {
		return e
	}
	var eligible bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tasks t JOIN projects p ON p.id=t.project_id WHERE t.project_id=$1 AND t.id=$2 AND p.archived_at IS NULL AND t.responsibility='customer' AND t.customer_email=$3)", project, m.TaskID, m.Recipient).Scan(&eligible); e != nil {
		return e
	}
	if !eligible {
		return finish("failed", "recipient_or_task_changed")
	}
	state, reason := sendCustomerMail(ctx, a.Config.SMTP, m)
	return finish(state, reason)
}
