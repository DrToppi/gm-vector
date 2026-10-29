package planner

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

const diagnosticLimit = 500
const diagnosticTTL = 15 * time.Minute

// Only these fields can leave the API. Ownership is held separately.
type DiagnosticEvent struct {
	Time       time.Time `json:"time"`
	RequestID  string    `json:"request_id"`
	Route      string    `json:"route"`
	Method     string    `json:"method"`
	Status     int       `json:"status"`
	DurationMS int64     `json:"duration_ms"`
}
type ownedDiagnosticEvent struct {
	event DiagnosticEvent
	owner string
}
type diagnosticBuffer struct {
	mu     sync.Mutex
	events []ownedDiagnosticEvent
}

func (b *diagnosticBuffer) prune(now time.Time) {
	kept := b.events[:0]
	for _, item := range b.events {
		if item.event.Time.After(now.Add(-diagnosticTTL)) {
			kept = append(kept, item)
		}
	}
	clear(b.events[len(kept):])
	b.events = kept
}
func (b *diagnosticBuffer) add(event DiagnosticEvent, owner string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prune(time.Now())
	if len(b.events) == diagnosticLimit {
		copy(b.events, b.events[1:])
		b.events = b.events[:diagnosticLimit-1]
	}
	b.events = append(b.events, ownedDiagnosticEvent{event, owner})
}
func (b *diagnosticBuffer) snapshot(now time.Time, owner string, admin bool) []DiagnosticEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prune(now)
	result := make([]DiagnosticEvent, 0)
	for _, item := range b.events {
		if admin || (owner != "" && item.owner == owner) {
			result = append(result, item.event)
		}
	}
	return result
}
func (a *App) getDiagnosticEvents(w http.ResponseWriter, r *http.Request, u Principal) error {
	if u.ID == "" || u.Token {
		return denied()
	}
	jsonResponse(w, http.StatusOK, map[string]any{"events": a.diagnosticEvents.snapshot(time.Now(), u.ID, u.Admin), "retention_minutes": 15, "capacity": diagnosticLimit})
	return nil
}

type diagnosticContextKey struct{}
type diagnosticRequest struct{ owner string }
type diagnosticResponse struct {
	http.ResponseWriter
	status int
}

func (w *diagnosticResponse) WriteHeader(status int) {
	if status >= 100 && status < 200 {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *diagnosticResponse) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}
func (w *diagnosticResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *diagnosticResponse) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func safeDiagnosticMethod(method string) string {
	switch method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
		return method
	}
	return "unknown"
}

// Pattern is set by ServeMux from registered routes, never from URL text.
// Fallback/static patterns contain no useful operation and are not exported.
func safeDiagnosticRoute(pattern string) string {
	_, route, found := strings.Cut(pattern, " ")
	if !found || !strings.HasPrefix(route, "/api/v1/") {
		return "unknown"
	}
	return route
}
