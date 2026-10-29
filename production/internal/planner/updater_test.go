package planner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func unixUpdater(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "gm-vector-rpc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "agent.sock")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	s := &http.Server{Handler: h}
	go s.Serve(l)
	t.Cleanup(func() { s.Close() })
	return socket
}
func updaterRequest(body string) *http.Request {
	r := httptest.NewRequest("POST", "https://synthetic.invalid/api/v1/admin/updates/check", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}
func updaterErrorCode(err error) int {
	var a appError
	if errors.As(err, &a) {
		return a.Status
	}
	return 0
}
func TestUpdaterDisabledAndRoleBoundary(t *testing.T) {
	a := &App{}
	admin := Principal{User: User{Admin: true}}
	for _, u := range []Principal{{}, {User: User{Admin: false}}, {User: User{Admin: true}, Token: true}} {
		for _, h := range []handler{a.updaterStatus, a.updaterCheck, a.updaterApply, a.updaterRecover} {
			if n := updaterErrorCode(h(httptest.NewRecorder(), updaterRequest(`{}`), u)); n != 403 {
				t.Fatalf("non-admin/session passed%d", n)
			}
		}
	}
	w := httptest.NewRecorder()
	if err := a.updaterStatus(w, updaterRequest(""), admin); err != nil {
		t.Fatal(err)
	}
	var state UpdaterStatus
	if json.Unmarshal(w.Body.Bytes(), &state) != nil || state.Enabled || state.State != "disabled" {
		t.Fatal("disabled contract")
	}
	if n := updaterErrorCode(a.updaterCheck(httptest.NewRecorder(), updaterRequest(`{"ref":"main"}`), admin)); n != 503 {
		t.Fatalf("disabled mutation%d", n)
	}
}
func TestUpdaterUnixRelayAndPrivacy(t *testing.T) {
	secret := strings.Repeat("a", 43)
	var calls atomic.Int32
	socket := unixUpdater(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "updater" || r.Header.Get("Authorization") != "Bearer "+secret {
			t.Error("Unix authorization")
		}
		if r.URL.Path != "/check" || r.Method != "POST" {
			t.Error("endpoint")
		}
		raw, _ := io.ReadAll(r.Body)
		if string(raw) != `{"ref":"main"}` {
			t.Errorf("request%s", raw)
		}
		calls.Add(1)
		w.WriteHeader(202)
		io.WriteString(w, `{"enabled":true,"state":"checking","phase":"fetch","message":"private credentials","logs":"secret","token":"secret","current_commit":"`+strings.Repeat("b", 40)+`","operation_id":"`+strings.Repeat("c", 32)+`"}`)
	})
	a := &App{Config: Config{UpdaterSocket: socket, UpdaterToken: secret}}
	w := httptest.NewRecorder()
	result, status, err := a.updaterCall(updaterRequest(`{"ref":"main"}`), "POST", "/check", map[string]string{"ref": "main"})
	if err == nil {
		jsonResponse(w, status, result)
	}
	if err != nil || w.Code != 202 || calls.Load() != 1 {
		t.Fatalf("relay:%v code%d", err, w.Code)
	}
	for _, v := range []string{"private", "credentials", "logs", "secret", secret} {
		if strings.Contains(w.Body.String(), v) {
			t.Fatalf("sensitive value%q", v)
		}
	}
	var s UpdaterStatus
	json.Unmarshal(w.Body.Bytes(), &s)
	if s.State != "checking" || s.Phase != "fetch" || s.CurrentCommit != strings.Repeat("b", 40) || s.Message != "Checking update source" {
		t.Fatal("safe status")
	}
}
func TestUpdaterRejectsBadInputBeforeService(t *testing.T) {
	var calls atomic.Int32
	socket := unixUpdater(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	a := &App{Config: Config{UpdaterSocket: socket, UpdaterToken: strings.Repeat("a", 43)}}
	admin := Principal{User: User{Admin: true}}
	for _, v := range []string{"", "--upload-pack=anything", "https://example.test/x", "../head", "main//x", "main/", "main.", "main.lock", "HEAD~1", "x@{1}", "a\nthing"} {
		b, _ := json.Marshal(map[string]string{"ref": v})
		if n := updaterErrorCode(a.updaterCheck(httptest.NewRecorder(), updaterRequest(string(b)), admin)); n != 422 {
			t.Fatalf("ref%q code%d", v, n)
		}
	}
	for _, v := range []string{`{"ref":"main","repository":"https://example.test"}`, `{"ref":"main"} {}`, `{}`} {
		if err := a.updaterCheck(httptest.NewRecorder(), updaterRequest(v), admin); err == nil {
			t.Fatal("invalid input")
		}
	}
	if n := updaterErrorCode(a.updaterCheck(httptest.NewRecorder(), updaterRequest(`{"ref":"`+strings.Repeat("a", 2000)+`"}`), admin)); n != 413 {
		t.Fatalf("body bound%d", n)
	}
	for _, v := range []string{`{"commit":"main","confirmation":"main"}`, `{"commit":"` + strings.Repeat("a", 40) + `","confirmation":"wrong"}`} {
		if err := a.updaterApply(httptest.NewRecorder(), updaterRequest(v), admin); err == nil {
			t.Fatal("invalid apply")
		}
	}
	if err := a.updaterRecover(httptest.NewRecorder(), updaterRequest(`{"operation_id":"../../state","confirmation":"../../state"}`), admin); err == nil {
		t.Fatal("path recovery")
	}
	if calls.Load() != 0 {
		t.Fatal("invalid reached service")
	}
}
func TestUpdaterServiceFailureIsBoundedAndSanitized(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   int
	}{
		{"oversized", 200, strings.Repeat("x", updaterMaxResponse+1), 503}, {"malformed", 200, `{"enabled":true,`, 503}, {"trailing", 200, `{"enabled":true,"state":"idle"} {}`, 503}, {"state", 200, `{"enabled":true,"state":"/private/password"}`, 503}, {"phase", 200, `{"enabled":true,"state":"idle","phase":"/private"}`, 503},
		{"unknownphase", 200, `{"enabled":true,"state":"idle","phase":"private_message"}`, 503}, {"commit", 200, `{"enabled":true,"state":"idle","candidate_commit":"--command"}`, 503}, {"operation", 200, `{"enabled":true,"state":"idle","operation_id":"../../state"}`, 503}, {"auth", 401, `private`, 503}, {"server", 500, `private`, 503}, {"redirect", 302, ``, 503}, {"conflict", 409, `private`, 409}, {"validation", 400, `private`, 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			socket := unixUpdater(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "http://127.0.0.1:1/private")
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			})
			a := &App{Config: Config{UpdaterSocket: socket, UpdaterToken: strings.Repeat("a", 43)}}
			_, _, err := a.updaterCall(updaterRequest(""), "GET", "/status", nil)
			if updaterErrorCode(err) != tc.want || strings.Contains(err.Error(), "private") {
				t.Fatalf("unsafe failure%v", err)
			}
		})
	}
	t.Run("deadline", func(t *testing.T) {
		socket := unixUpdater(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		a := &App{Config: Config{UpdaterSocket: socket, UpdaterToken: strings.Repeat("a", 43)}}
		start := time.Now()
		_, _, err := a.updaterCall(updaterRequest("").WithContext(ctx), "GET", "/status", nil)
		if updaterErrorCode(err) != 503 || time.Since(start) > time.Second {
			t.Fatal("unbounded cancellation")
		}
	})
	a := &App{Config: Config{UpdaterSocket: "/missing/gm-vector.sock", UpdaterToken: strings.Repeat("a", 43)}}
	if _, _, err := a.updaterCall(updaterRequest(""), "GET", "/status", nil); updaterErrorCode(err) != 503 {
		t.Fatal("unavailable service")
	}
}
func TestUpdaterConfigurationSecrets(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "agent.sock")
	file := filepath.Join(t.TempDir(), "updater.token")
	if _, _, e := loadUpdater("", ""); e != nil {
		t.Fatal(e)
	}
	for _, p := range [][2]string{{socket, ""}, {"", file}, {"relative.sock", file}, {socket + "/../a", file}} {
		if _, _, e := loadUpdater(p[0], p[1]); e == nil {
			t.Fatal("bad config")
		}
	}
	os.WriteFile(file, []byte(strings.Repeat("a", 43)), 0600)
	if s, k, e := loadUpdater(socket, file); e != nil || s != socket || len(k) != 43 {
		t.Fatal("valid config", e)
	}
	os.Chmod(file, 0644)
	if _, _, e := loadUpdater(socket, file); e == nil {
		t.Fatal("public token")
	}
	os.Chmod(file, 0600)
	link := file + ".link"
	os.Symlink(file, link)
	if _, _, e := loadUpdater(socket, link); e == nil {
		t.Fatal("symlink token")
	}
	os.WriteFile(file, []byte(strings.Repeat("x", 129)), 0600)
	if _, _, e := loadUpdater(socket, file); e == nil {
		t.Fatal("large token")
	}
	os.WriteFile(file, []byte("bad token"), 0600)
	if _, _, e := loadUpdater(socket, file); e == nil {
		t.Fatal("invalid token")
	}
}

// Invoked inside TestAPIIntegration's private migrated schema and fixture user.
func testUpdaterAudit(t *testing.T, app *App, user User) {
	t.Helper()
	ctx := context.Background()
	var calls atomic.Int32
	socket := unixUpdater(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(202)
		io.WriteString(w, `{"enabled":true,"state":"checking","phase":"queued"}`)
	})
	probe := &App{Store: app.Store, Config: Config{UpdaterSocket: socket, UpdaterToken: strings.Repeat("a", 43)}}
	cases := []struct {
		handler      handler
		body, action string
	}{
		{probe.updaterCheck, `{"ref":"main"}`, "check"},
		{probe.updaterApply, `{"commit":"` + strings.Repeat("b", 40) + `","confirmation":"` + strings.Repeat("b", 40) + `"}`, "apply"},
		{probe.updaterRecover, `{"operation_id":"` + strings.Repeat("c", 32) + `","confirmation":"` + strings.Repeat("c", 32) + `"}`, "recover"},
	}
	for _, tc := range cases {
		var before int
		err := app.Store.DB.QueryRow(ctx, "SELECT count(*) FROM audit WHERE actor=$1 AND action=$2", user.ID, "updater."+tc.action+".requested").Scan(&before)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		if err = tc.handler(w, updaterRequest(tc.body), Principal{User: user}); err != nil || w.Code != 202 {
			t.Fatal("audit dispatch", err, w.Code)
		}
		var actor string
		var details []byte
		err = app.Store.DB.QueryRow(ctx, "SELECT actor,details FROM audit WHERE actor=$1 AND action=$2 ORDER BY id DESC LIMIT 1", user.ID, "updater."+tc.action+".requested").Scan(&actor, &details)
		if err != nil || actor != user.ID {
			t.Fatal("missing admin attribution", err)
		}
		var count int
		app.Store.DB.QueryRow(ctx, "SELECT count(*) FROM audit WHERE actor=$1 AND action=$2", user.ID, "updater."+tc.action+".requested").Scan(&count)
		if count != before+1 || strings.Contains(string(details), "confirmation") || strings.Contains(string(details), "token") {
			t.Fatal("unsafe/missing audit", string(details))
		}
	}
	// The action cannot reach the privileged host if the database audit write fails.
	failing := &App{Store: app.Store, Config: probe.Config}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	before := calls.Load()
	if err := failing.updaterCheck(httptest.NewRecorder(), updaterRequest(`{"ref":"main"}`).WithContext(cancelled), Principal{User: user}); err == nil {
		t.Fatal("audit failure accepted")
	}
	if calls.Load() != before {
		t.Fatal("audit failure still dispatched")
	}
}
