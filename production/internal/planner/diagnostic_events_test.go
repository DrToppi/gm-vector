package planner

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDiagnosticBufferBoundsOwnershipAndExpiry(t *testing.T) {
	var b diagnosticBuffer
	now := time.Now()
	b.add(DiagnosticEvent{Time: now, RequestID: "anonymous"}, "")
	b.add(DiagnosticEvent{Time: now, RequestID: "own"}, "alice")
	b.add(DiagnosticEvent{Time: now, RequestID: "other"}, "bob")
	got := b.snapshot(now, "alice", false)
	if len(got) != 1 || got[0].RequestID != "own" {
		t.Fatalf("ownership filter: %+v", got)
	}
	if len(b.snapshot(now, "", false)) != 0 || len(b.snapshot(now, "", true)) != 3 {
		t.Fatal("anonymous/admin visibility")
	}
	raw, _ := json.Marshal(b.snapshot(now, "alice", false))
	if strings.Contains(string(raw), "alice") || strings.Contains(string(raw), "owner") {
		t.Fatal("owner exported")
	}
	for i := 0; i < 600; i++ {
		b.add(DiagnosticEvent{Time: now, Status: i}, "alice")
	}
	got = b.snapshot(now, "alice", false)
	if len(got) != 500 || got[0].Status != 100 || got[499].Status != 599 {
		t.Fatal("bounded FIFO failed")
	}
	if len(b.snapshot(now.Add(diagnosticTTL), "", true)) != 0 {
		t.Fatal("expired events retained")
	}
}
func TestDiagnosticBufferConcurrentAccess(t *testing.T) {
	var b diagnosticBuffer
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				b.add(DiagnosticEvent{Time: time.Now()}, "own")
				b.snapshot(time.Now(), "own", false)
			}
		}()
	}
	wg.Wait()
	if len(b.snapshot(time.Now(), "own", false)) != diagnosticLimit {
		t.Fatal("limit")
	}
}
func TestDiagnosticMiddlewareSanitizesURLAndCapturesPanic(t *testing.T) {
	a := &App{Config: Config{Origin: "https://localhost:9443", WebDir: t.TempDir()}}
	h := a.Handler()
	for _, path := range []string{"/api/v1/projects/private-project?token=SECRET", "/api/PRIVATE?password=SECRET", "/api/v1/auth/status?token=SECRET"} {
		r := httptest.NewRequest("GET", "https://localhost:9443"+path, nil)
		r.Header.Set("Authorization", "SECRET")
		r.Header.Set("X-Request-ID", "UNTRUSTED")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Header().Get("X-Request-ID") == "UNTRUSTED" {
			t.Fatal("trusted request id")
		}
	}
	events := a.diagnosticEvents.snapshot(time.Now(), "", true)
	if len(events) != 3 || events[0].Route != "/api/v1/projects/{id}" || events[1].Route != "unknown" || events[2].Status != 500 {
		t.Fatalf("events: %+v", events)
	}
	raw, _ := json.Marshal(events)
	for _, secret := range []string{"private-project", "PRIVATE", "SECRET", "token=", "UNTRUSTED"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("leaked", secret)
		}
	}
	for _, event := range events {
		if event.RequestID == "" || event.DurationMS < 0 {
			t.Fatal("missing metadata")
		}
	}
}
func TestDiagnosticHandlerRejectsUnauthenticatedAndToken(t *testing.T) {
	a := &App{}
	for _, p := range []Principal{{}, {ID: "admin", Admin: true, Token: true}} {
		if e := a.getDiagnosticEvents(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), p); e == nil {
			t.Fatal("access allowed")
		}
	}
}
