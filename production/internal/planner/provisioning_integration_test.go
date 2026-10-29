package planner

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/mail"
	"net/textproto"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// Called only inside TestAPIIntegration's fresh PostgreSQL schema and TLS server.
func runProvisioningIntegration(t *testing.T, a *App, request func(string, string, any, *testSession, *string) (int, json.RawMessage), admin *testSession) {
	t.Helper()
	ctx := context.Background()
	oldSMTP, oldLookup := a.Config.SMTP, a.directoryLookup
	defer func() { a.Config.SMTP = oldSMTP; a.directoryLookup = oldLookup }()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := a.Store.DB.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if e := a.Store.DB.QueryRow(ctx, sql, args...).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	expect := func(want int, method, path string, body any, s *testSession, csrf *string) json.RawMessage {
		t.Helper()
		status, result := request(method, path, body, s, csrf)
		if status != want {
			t.Fatalf("%s %s: status %d, want %d: %s", method, path, status, want, result)
		}
		return result
	}
	a.directoryLookup = func(_ context.Context, e Employee) (DirectoryPerson, error) {
		return DirectoryPerson{ID: e.ExternalID, Provider: e.Provider, Name: e.Name, Email: "verified@example.test"}, nil
	}
	created := []string{}
	users := []string{}
	defer func() {
		for _, id := range created {
			exec("DELETE FROM outbox WHERE id IN (SELECT id::text FROM employee_invitations WHERE employee_id=$1)", id)
			exec("DELETE FROM employee_invitations WHERE employee_id=$1", id)
			exec("DELETE FROM employees WHERE id=$1", id)
		}
		for _, id := range users {
			exec("DELETE FROM users WHERE id=$1", id)
		}
	}()
	employee := func() string {
		id := newID()
		created = append(created, id)
		exec("INSERT INTO employees(id,name,daily_minutes,provider,external_id,email) VALUES($1,'Provision test',480,'fixture',$1,'untrusted@example.test')", id)
		return id
	}
	path := func(id string) string { return "/employees/" + id + "/invite" }
	id := employee()
	op := newUUIDForProvisioning()
	body := map[string]any{"id": op}
	before := count("SELECT count(*) FROM users")
	a.Config.SMTP.Enabled = false
	expect(503, "POST", path(id), body, admin, nil)
	if count("SELECT count(*) FROM users") != before || count("SELECT count(*) FROM employee_invitations WHERE employee_id=$1", id) != 0 {
		t.Fatal("disabled SMTP mutated account/invitation")
	}
	memberID := newID()
	users = append(users, memberID)
	exec("INSERT INTO users(id,username,name,password_hash,totp,active) VALUES($1,$1,'Nonadmin','unused','',true)", memberID)
	member := testSession{token(), token()}
	exec("INSERT INTO sessions(hash,user_id,csrf) VALUES($1,$2,$3)", hash(member.cookie), memberID, member.csrf)
	expect(403, "GET", path(id), nil, &member, nil)
	expect(403, "POST", path(id), body, &member, nil)
	badCSRF := "bad"
	expect(403, "POST", path(id), body, admin, &badCSRF)

	a.Config.SMTP.Enabled = true
	a.directoryLookup = func(_ context.Context, e Employee) (DirectoryPerson, error) {
		return DirectoryPerson{ID: e.ExternalID, Provider: e.Provider}, nil
	}
	expect(422, "POST", path(id), body, admin, nil)
	if count("SELECT count(*) FROM employee_invitations WHERE employee_id=$1", id) != 0 {
		t.Fatal("missing directory email created invitation")
	}
	a.directoryLookup = func(_ context.Context, e Employee) (DirectoryPerson, error) {
		return DirectoryPerson{ID: e.ExternalID, Provider: e.Provider, Email: "verified@example.test"}, nil
	}
	raw := expect(201, "POST", path(id), body, admin, nil)
	var inv InviteStatus
	if e := json.Unmarshal(raw, &inv); e != nil {
		t.Fatal(e)
	}
	users = append(users, inv.UserID)
	if inv.Email != "verified@example.test" || inv.State != "pending" {
		t.Fatal("unverified recipient or active account")
	}
	for _, s := range []string{"token", "secret", "password"} {
		if strings.Contains(string(raw), s) {
			t.Fatal("invitation API exposed secret field")
		}
	}
	expect(200, "POST", path(id), body, admin, nil)
	expect(409, "POST", path(id), map[string]any{"id": op, "reissue": true}, admin, nil)
	if count("SELECT count(*) FROM outbox WHERE id=$1", op) != 1 {
		t.Fatal("replay duplicated outbox")
	}
	get := expect(200, "GET", path(id), nil, admin, nil)
	if strings.Contains(string(get), "secret") || strings.Contains(string(get), "token") {
		t.Fatal("GET exposed enrollment secret")
	}
	var payload []byte
	if e := a.Store.DB.QueryRow(ctx, "SELECT payload FROM outbox WHERE id=$1", op).Scan(&payload); e != nil {
		t.Fatal(e)
	}
	var sealed struct {
		Secret []byte `json:"secret"`
	}
	if e := json.Unmarshal(payload, &sealed); e != nil {
		t.Fatal(e)
	}
	plain, e := unseal(a.Config.Secret, sealed.Secret)
	if e != nil {
		t.Fatal(e)
	}
	oldToken := string(plain)
	if strings.Contains(string(payload), oldToken) {
		t.Fatal("plaintext token in outbox")
	}
	pending := testSession{token(), token()}
	exec("INSERT INTO sessions(hash,user_id,csrf) VALUES($1,$2,$3)", hash(pending.cookie), inv.UserID, pending.csrf)
	expect(401, "GET", "/auth/me", nil, &pending, nil)
	newOp := newUUIDForProvisioning()
	raw = expect(201, "POST", path(id), map[string]any{"id": newOp, "reissue": true}, admin, nil)
	if count("SELECT count(*) FROM outbox WHERE id=$1 AND payload='{}' AND state='failed'", op) != 1 {
		t.Fatal("reissue retained old encrypted token")
	}
	expect(403, "POST", "/auth/bootstrap", map[string]string{"token": oldToken, "username": inv.Username, "name": "Invite test", "password": "correct horse battery staple"}, nil, nil)
	if e = a.Store.DB.QueryRow(ctx, "SELECT payload FROM outbox WHERE id=$1", newOp).Scan(&payload); e != nil {
		t.Fatal(e)
	}
	json.Unmarshal(payload, &sealed)
	plain, e = unseal(a.Config.Secret, sealed.Secret)
	if e != nil {
		t.Fatal(e)
	}
	enroll := expect(200, "POST", "/auth/bootstrap", map[string]string{"token": string(plain), "username": inv.Username, "name": "Invite test", "password": "own password long enough"}, nil, nil)
	expect(401, "GET", "/auth/me", nil, &pending, nil)
	var setup struct {
		Secret string `json:"secret"`
	}
	json.Unmarshal(enroll, &setup)
	activated := testSession{}
	expect(200, "POST", "/auth/bootstrap/confirm", map[string]string{"token": string(plain), "code": totpAt(setup.Secret, time.Now().Unix()/30)}, &activated, nil)
	expect(200, "GET", "/auth/me", nil, &activated, nil)
	if count("SELECT count(*) FROM memberships WHERE user_id=$1", inv.UserID) != 0 {
		t.Fatal("invitation granted project membership")
	}
	if count("SELECT count(*) FROM outbox WHERE id=$1 AND payload='{}'", newOp) != 1 {
		t.Fatal("activation retained encrypted token")
	}
	expect(409, "POST", path(id), map[string]any{"id": newUUIDForProvisioning(), "reissue": true}, admin, nil)
	// Editing local capacity must not depend on directory availability or trust client email.
	lookupCalled := false
	a.directoryLookup = func(context.Context, Employee) (DirectoryPerson, error) {
		lookupCalled = true
		return DirectoryPerson{}, errors.New("fixture directory offline")
	}
	expect(200, "POST", "/employees", map[string]any{"id": id, "name": "Provision test", "daily_minutes": 360, "user_id": inv.UserID, "provider": "fixture", "external_id": id, "email": "attacker@example.test"}, admin, nil)
	if lookupCalled {
		t.Fatal("local capacity update required external directory")
	}
	if count("SELECT count(*) FROM employees WHERE id=$1 AND daily_minutes=360 AND email='verified@example.test' AND user_id=$2", id, inv.UserID) != 1 {
		t.Fatal("local capacity update changed trusted identity")
	}
	expect(409, "POST", "/employees", map[string]any{"id": id, "name": "Provision test", "daily_minutes": 360, "user_id": "", "provider": "fixture", "external_id": id}, admin, nil)
	a.directoryLookup = func(_ context.Context, emp Employee) (DirectoryPerson, error) {
		return DirectoryPerson{ID: emp.ExternalID, Provider: emp.Provider, Email: "verified@example.test"}, nil
	}
	expiredID := employee()
	expiredOp := newUUIDForProvisioning()
	raw = expect(201, "POST", path(expiredID), map[string]string{"id": expiredOp}, admin, nil)
	json.Unmarshal(raw, &inv)
	users = append(users, inv.UserID)
	exec("UPDATE employee_invitations SET expires_at=now()-interval '1 second' WHERE id=$1", expiredOp)
	a.Config.SMTP.Enabled = false
	if e = a.invitationMailTick(ctx); e != nil {
		t.Fatal(e)
	}
	if count("SELECT count(*) FROM outbox WHERE id=$1 AND payload='{}' AND state='failed'", expiredOp) != 1 || count("SELECT count(*) FROM enrollments WHERE user_id=$1", inv.UserID) != 0 {
		t.Fatal("expiry cleanup retained token")
	}
	a.Config.SMTP.Enabled = true
	// Exercise the durable worker against a loopback-only trusted TLS SMTP peer.
	mailID, mailOp := employee(), newUUIDForProvisioning()
	raw = expect(201, "POST", path(mailID), map[string]string{"id": mailOp}, admin, nil)
	json.Unmarshal(raw, &inv)
	users = append(users, inv.UserID)
	config, received, stop := provisioningSMTPPeer(t)
	defer stop()
	a.Config.SMTP = config
	if e = a.invitationMailTick(ctx); e != nil {
		t.Fatal(e)
	}
	var wire string
	select {
	case wire = <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("invitation worker did not deliver to local TLS peer")
	}
	parsedMail, e := mail.ReadMessage(strings.NewReader(wire))
	if e != nil {
		t.Fatal(e)
	}
	if parsedMail.Header.Get("To") != "verified@example.test" {
		t.Fatal("invitation sent to unverified recipient")
	}
	decoded, e := io.ReadAll(base64.NewDecoder(base64.StdEncoding, parsedMail.Body))
	if e != nil {
		t.Fatal(e)
	}
	var link *url.URL
	for _, line := range strings.Split(string(decoded), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), a.Config.Origin+"/#setup=") {
			link, e = url.Parse(strings.TrimSpace(line))
			if e != nil {
				t.Fatal(e)
			}
		}
	}
	if link == nil {
		t.Fatal("invitation has no setup link")
	}
	fragment, e := url.ParseQuery(link.Fragment)
	if e != nil {
		t.Fatal(e)
	}
	if len(fragment.Get("setup")) < 32 || fragment.Get("login") != inv.Username {
		t.Fatal("invitation setup fragment invalid")
	}
	if count("SELECT count(*) FROM enrollments WHERE hash=$1 AND user_id=$2", hash(fragment.Get("setup")), inv.UserID) != 1 {
		t.Fatal("delivered link does not match pending enrollment")
	}
	if count("SELECT count(*) FROM outbox WHERE id=$1 AND state='sent' AND payload='{}'", mailOp) != 1 {
		t.Fatal("delivered worker record retained ciphertext or failed state")
	}
	// Release this pending reservation before testing the final licence seat.
	exec("UPDATE employee_invitations SET expires_at=now()-interval '1 second' WHERE id=$1", mailOp)
	a.Config.SMTP.Enabled = false
	if e = a.invitationMailTick(ctx); e != nil {
		t.Fatal(e)
	}
	a.Config.SMTP.Enabled = true
	// Force both requests to resolve the same pre-creation snapshot before locking.
	concurrentID, concurrentOp := employee(), newUUIDForProvisioning()
	var lookups sync.WaitGroup
	lookups.Add(2)
	a.directoryLookup = func(_ context.Context, emp Employee) (DirectoryPerson, error) {
		lookups.Done()
		lookups.Wait()
		return DirectoryPerson{ID: emp.ExternalID, Provider: emp.Provider, Email: "verified@example.test"}, nil
	}
	var repeats sync.WaitGroup
	replayed := make(chan int, 2)
	for n := 0; n < 2; n++ {
		repeats.Add(1)
		go func() {
			defer repeats.Done()
			session := *admin
			code, _ := request("POST", path(concurrentID), map[string]string{"id": concurrentOp}, &session, nil)
			replayed <- code
		}()
	}
	repeats.Wait()
	close(replayed)
	var concurrentUID string
	a.Store.DB.QueryRow(ctx, "SELECT coalesce(user_id,'') FROM employees WHERE id=$1", concurrentID).Scan(&concurrentUID)
	if concurrentUID != "" {
		users = append(users, concurrentUID)
	}
	for status := range replayed {
		if status != 200 && status != 201 {
			t.Fatalf("concurrent same UUID returned %d instead of same operation", status)
		}
	}
	if count("SELECT count(*) FROM outbox WHERE id=$1", concurrentOp) != 1 {
		t.Fatal("same UUID concurrent delivery duplicate")
	}
	a.directoryLookup = func(_ context.Context, emp Employee) (DirectoryPerson, error) {
		return DirectoryPerson{ID: emp.ExternalID, Provider: emp.Provider, Email: "verified@example.test"}, nil
	}
	// Independent employees can be invited concurrently without artificial seat limits.
	first, second := employee(), employee()
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for _, eid := range []string{first, second} {
		wg.Add(1)
		go func(eid string) {
			defer wg.Done()
			session := *admin
			status, _ := request("POST", path(eid), map[string]string{"id": newUUIDForProvisioning()}, &session, nil)
			results <- status
		}(eid)
	}
	wg.Wait()
	close(results)
	success := 0
	for status := range results {
		if status == 201 {
			success++
		} else if status != 422 {
			t.Fatalf("unexpected concurrent invitation status %d", status)
		}
	}
	if success != 2 {
		t.Fatal("concurrent independent invitations failed")
	}
	for _, eid := range []string{first, second} {
		var uid string
		a.Store.DB.QueryRow(ctx, "SELECT coalesce(user_id,'') FROM employees WHERE id=$1", eid).Scan(&uid)
		if uid != "" {
			users = append(users, uid)
		}
	}
	t.Log("Provisioning API: permissions, disabled SMTP atomicity, verified recipient, idempotency, encrypted queue, reissue, password+TOTP gating, expiry cleanup and concurrent invitations verified")
}

func newUUIDForProvisioning() string {
	s := newID()
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32]
}

// No DNS or external SMTP: only a private loopback listener and test CA.
func provisioningSMTPPeer(t *testing.T) (SMTPConfig, <-chan string, func()) {
	t.Helper()
	cert, roots := smtpTestCertificate(t)
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	received := make(chan string, 1)
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		secure := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
		if secure.Handshake() != nil {
			return
		}
		peer := textproto.NewConn(secure)
		peer.PrintfLine("220 localhost test SMTP")
		for {
			line, e := peer.ReadLine()
			if e != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO "):
				peer.PrintfLine("250 localhost")
			case strings.HasPrefix(line, "MAIL FROM:") || strings.HasPrefix(line, "RCPT TO:"):
				peer.PrintfLine("250 accepted")
			case line == "DATA":
				peer.PrintfLine("354 message follows")
				body, e := peer.ReadDotBytes()
				if e != nil {
					return
				}
				received <- string(body)
				peer.PrintfLine("250 queued")
			case line == "QUIT":
				peer.PrintfLine("221 bye")
				return
			default:
				peer.PrintfLine("500 unexpected command")
			}
		}
	}()
	return SMTPConfig{Enabled: true, Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, Security: "tls", From: "planner@example.test", roots: roots}, received, func() { listener.Close() }
}
