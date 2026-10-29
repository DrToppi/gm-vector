package planner

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"io"
	"math/big"
	"net"
	"net/mail"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

func smtpTestCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, roots
}

// The peer is always loopback and processes only one connection. No mail leaves
// this process. Transcript checks also ensure cleartext mode cannot send DATA.
func TestCustomerMailSMTPDelivery(t *testing.T) {
	cert, roots := smtpTestCertificate(t)
	for _, tc := range []struct {
		name, security, outcome, wantState, wantReason string
		untrusted                                      bool
	}{
		{"implicit TLS accepted", "tls", "accept", "sent", "", false},
		{"STARTTLS accepted", "starttls", "accept", "sent", "", false},
		{"STARTTLS required", "starttls", "no_tls", "failed", "smtp_tls_required", false},
		{"untrusted certificate", "tls", "accept", "failed", "smtp_connection_failed", true},
		{"missing DATA confirmation", "tls", "disconnect", "uncertain", "smtp_confirmation_missing", false},
		{"explicit DATA rejection", "tls", "reject", "failed", "smtp_message_rejected", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
			message := make(chan string, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				if tc.security == "tls" {
					secure := tls.Server(conn, tlsConfig)
					if secure.Handshake() != nil {
						return
					}
					conn = secure
				}
				peer := textproto.NewConn(conn)
				peer.PrintfLine("220 localhost test SMTP")
				for {
					line, err := peer.ReadLine()
					if err != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO "):
						if tc.security == "starttls" && tc.outcome != "no_tls" {
							peer.PrintfLine("250-localhost\r\n250 STARTTLS")
						} else {
							peer.PrintfLine("250 localhost")
						}
					case line == "STARTTLS":
						peer.PrintfLine("220 Begin TLS")
						secure := tls.Server(conn, tlsConfig)
						if secure.Handshake() != nil {
							return
						}
						conn = secure
						peer = textproto.NewConn(conn)
					case strings.HasPrefix(line, "MAIL FROM:") || strings.HasPrefix(line, "RCPT TO:"):
						peer.PrintfLine("250 accepted")
					case line == "DATA":
						peer.PrintfLine("354 message follows")
						body, err := peer.ReadDotBytes()
						if err != nil {
							return
						}
						message <- string(body)
						if tc.outcome == "disconnect" {
							return
						}
						if tc.outcome == "reject" {
							peer.PrintfLine("550 rejected")
						} else {
							peer.PrintfLine("250 queued")
						}
					case line == "QUIT":
						peer.PrintfLine("221 bye")
						return
					default:
						peer.PrintfLine("500 unexpected command")
					}
				}
			}()
			pool := roots
			if tc.untrusted {
				pool = x509.NewCertPool()
			}
			config := SMTPConfig{Enabled: true, Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, Security: tc.security, From: "planner@example.test", roots: pool}
			comment := TaskComment{ID: "22222222-2222-4222-8222-222222222222", ProjectID: "project", TaskID: "task", Recipient: "customer@example.test", AuthorName: "Автор\r\nBcc: attacker@example.test", Text: "Комментарий <b>без HTML</b>\n.\nСтрока"}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			state, reason := sendCustomerMail(ctx, config, comment)
			if state != tc.wantState || reason != tc.wantReason {
				t.Errorf("delivery = %s/%s, want %s/%s", state, reason, tc.wantState, tc.wantReason)
			}
			select {
			case <-finished:
			case <-time.After(6 * time.Second):
				t.Fatal("SMTP peer did not stop")
			}
			if tc.untrusted || tc.outcome == "no_tls" {
				select {
				case body := <-message:
					t.Fatalf("sent DATA without trusted TLS: %q", body)
				default:
				}
				return
			}
			select {
			case body := <-message:
				parsed, err := mail.ReadMessage(strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				if parsed.Header.Get("Bcc") != "" || parsed.Header.Get("Content-Type") != "text/plain; charset=UTF-8" {
					t.Fatal("unsafe MIME headers", parsed.Header)
				}
				decoded, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, parsed.Body))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(decoded), comment.Text) || !strings.Contains(string(decoded), comment.AuthorName) {
					t.Fatalf("comment not preserved in plain-text body: %q", decoded)
				}
			default:
				t.Error("SMTP did not receive message")
			}
		})
	}
}

func TestCustomerMailRejectsUnsafeConfiguration(t *testing.T) {
	base := SMTPConfig{Enabled: true, Host: "localhost", Port: 465, Security: "tls", From: "planner@example.test"}
	for _, mutate := range []func(*SMTPConfig){
		func(c *SMTPConfig) { c.From = "planner@example.test\r\nBcc: other@example.test" },
		func(c *SMTPConfig) { c.Security = "plain" },
		func(c *SMTPConfig) { c.Username = "user" },
		func(c *SMTPConfig) { c.Host = "localhost\r\nQUIT" },
	} {
		c := base
		mutate(&c)
		if validateSMTP(c) == nil {
			t.Errorf("unsafe config accepted: %+v", c)
		}
	}
	state, reason := sendCustomerMail(context.Background(), base, TaskComment{Recipient: "customer@example.test\r\nBcc: other@example.test"})
	if state != "failed" || reason != "invalid_mail_configuration" {
		t.Fatalf("unsafe recipient accepted: %s/%s", state, reason)
	}
}
