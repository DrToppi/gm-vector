package planner

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Uses only its own disposable schemas; never closes/stops any shared database.
func TestSecurityAuditResilience(t *testing.T) {
	dsn := os.Getenv("GMA_TEST_DSN")
	if dsn == "" {
		t.Skip("explicit isolated test database required")
	}
	parsed, e := url.Parse(dsn)
	if e != nil || parsed.Path != "/gma_planner_test" || parsed.Hostname() != "127.0.0.1" || parsed.Port() != "15432" {
		t.Fatal("refusing non-audit database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	admin, e := Open(ctx, dsn)
	if e != nil {
		t.Fatal("isolated DB unavailable")
	}
	t.Cleanup(admin.DB.Close)
	fresh := func() *Store {
		name := "resilience_" + newID()
		if _, e := admin.DB.Exec(ctx, "CREATE SCHEMA "+name); e != nil {
			t.Fatal("schema creation failed")
		}
		t.Cleanup(func() {
			_, e := admin.DB.Exec(context.Background(), "DROP SCHEMA "+name+" CASCADE")
			if e != nil {
				t.Error("schema cleanup failed")
			}
		})
		u := *parsed
		q := u.Query()
		q.Set("search_path", name)
		u.RawQuery = q.Encode()
		s, e := Open(ctx, u.String())
		if e != nil {
			t.Fatal("schema connection failed")
		}
		t.Cleanup(s.DB.Close)
		return s
	}
	t.Run("concurrent migrations and checksum rejection", func(t *testing.T) {
		s := fresh()
		var wg sync.WaitGroup
		results := make(chan error, 4)
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); results <- s.Migrate(ctx) }()
		}
		wg.Wait()
		close(results)
		for e := range results {
			if e != nil {
				t.Fatal("concurrent migration failed", e)
			}
		}
		var n int
		if e := s.DB.QueryRow(ctx, "SELECT count(*) FROM schema_versions").Scan(&n); e != nil || n != 7 {
			t.Fatal("expected exactly seven migrations")
		}
		if _, e := s.DB.Exec(ctx, "UPDATE schema_versions SET checksum='synthetic-corruption' WHERE version=3"); e != nil {
			t.Fatal(e)
		}
		if e := s.Migrate(ctx); e == nil || !strings.Contains(e.Error(), "checksum changed") {
			t.Fatal("checksum mismatch not rejected")
		}
		t.Log("PASS: four concurrent migrate calls, exactly seven versions, checksum tamper fails closed")
	})
	t.Run("partial migration transaction rollback", func(t *testing.T) {
		s := fresh()
		if _, e := s.DB.Exec(ctx, "CREATE TABLE users (synthetic_conflict int)"); e != nil {
			t.Fatal(e)
		}
		if e := s.Migrate(ctx); e == nil {
			t.Fatal("expected schema conflict")
		}
		var n int
		if e := s.DB.QueryRow(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name IN ('settings','schema_versions','enrollments')").Scan(&n); e != nil || n != 0 {
			t.Fatal("partial migration persisted")
		}
		if _, e := s.DB.Exec(ctx, "DROP TABLE users"); e != nil {
			t.Fatal(e)
		}
		if e := s.Migrate(ctx); e != nil {
			t.Fatal("retry failed", e)
		}
		t.Log("PASS: injected SQL failure rolls back all migration statements; retry succeeds")
	})
	t.Run("bounded request sizes and disabled outbox", func(t *testing.T) {
		s := fresh()
		if e := s.Migrate(ctx); e != nil {
			t.Fatal(e)
		}
		listener, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		defer listener.Close()
		var connections atomic.Int64
		go func() {
			for {
				c, e := listener.Accept()
				if e != nil {
					return
				}
				connections.Add(1)
				c.Close()
			}
		}()
		_, port, _ := net.SplitHostPort(listener.Addr().String())
		pn, _ := strconv.Atoi(port)

		a, e := NewApp(s, Config{Origin: "https://audit.invalid", WebDir: t.TempDir(), Secret: random(32), Bootstrap: token(), SMTP: SMTPConfig{Enabled: false, Host: "127.0.0.1", Port: pn}})
		if e != nil {
			t.Fatal(e)
		}
		for _, tc := range []struct {
			name, body string
			status     int
		}{
			{"at cap", strings.Repeat(" ", (5<<20)-2) + "{}", 422},
			{"over cap", strings.Repeat(" ", (5<<20)+1) + "{}", 413},
			{"unknown field", `{"unknown":true}`, 422},
			{"trailing JSON", `{} {}`, 422},
		} {
			r := httptest.NewRequest("POST", "https://audit.invalid/api/v1/auth/bootstrap", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", a.Config.Origin)
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("%s status=%d", tc.name, w.Code)
			}
		}
		// Use the production main.go MaxHeaderBytes setting; test only loopback.
		server := httptest.NewUnstartedServer(a.Handler())
		server.Config.MaxHeaderBytes = 16384
		server.Start()
		defer server.Close()
		req, _ := http.NewRequest("GET", server.URL+"/healthz", nil)
		req.Header.Set("X-Synthetic-Large", strings.Repeat("x", 32768))
		res, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != 431 {
			t.Fatal("header limit", res.StatusCode)
		}
		for _, provider := range []string{"smtp", "smtp_invitation", "graph", "external_directory"} {
			if _, e := s.DB.Exec(ctx, "INSERT INTO outbox(id,provider,state,payload) VALUES($1,$1,'queued','{}')", provider); e != nil {
				t.Fatal(e)
			}
		}
		if e := a.workerTick(ctx); e != nil {
			t.Fatal(e)
		}
		var n int
		if e := s.DB.QueryRow(ctx, "SELECT count(*) FROM outbox WHERE state='queued' AND attempts=0").Scan(&n); e != nil || n != 4 {
			t.Fatal("disabled outbox advanced", n)
		}
		if connections.Load() != 0 {
			t.Fatal("disabled SMTP dialed listener")
		}
		t.Log("PASS: 5MiB accepted by transport / over rejected413, unknown/trailing422, 32KiB header431; disabled worker leaves four queues untouched and no SMTP connection")
	})
	t.Run("unavailable own pool API behavior", func(t *testing.T) {
		s := fresh()
		if e := s.Migrate(ctx); e != nil {
			t.Fatal(e)
		}

		a, e := NewApp(s, Config{Origin: "https://audit.invalid", WebDir: t.TempDir(), Secret: random(32), Bootstrap: token()})
		if e != nil {
			t.Fatal(e)
		}
		s.DB.Close()
		for path, status := range map[string]int{"/readyz": 503, "/healthz": 200, "/api/v1/auth/status": 500} {
			r := httptest.NewRequest("GET", "https://audit.invalid"+path, nil)
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, r)
			if w.Code != status {
				t.Fatalf("%s: status %d", path, w.Code)
			}
			if strings.Contains(w.Body.String(), "closed pool") || strings.Contains(w.Body.String(), "postgres") {
				t.Fatal("internal error exposed")
			}
		}
		t.Log("PASS: unavailable own DB pool returns ready503/health200/status500, generic error; this does not simulate TCP blackhole or full disk")
	})
}

// Finite graph corpus: all inputs remain at or one item beyond declared bounds.
func TestSecurityAuditGraphBounds(t *testing.T) {
	makeProject := func(n int) Project {
		p, _, _ := fixture()
		p.Tasks = nil
		p.Links = nil
		p.People = nil
		for i := 0; i < n; i++ {
			p.Tasks = append(p.Tasks, Task{ID: fmt.Sprintf("t%d", i), Title: "Synthetic", Status: "todo", Responsibility: "internal"})
		}
		return p
	}
	p := makeProject(300)
	for gap := 1; len(p.Links) < 1500; gap++ {
		for i := 0; i+gap < 300 && len(p.Links) < 1500; i++ {
			p.Links = append(p.Links, Link{From: fmt.Sprintf("t%d", i), To: fmt.Sprintf("t%d", i+gap), Mode: "after"})
		}
	}
	if _, e := validateProject(&p); e != nil {
		t.Fatal("valid maximum graph rejected", e)
	}
	p.Links = append(p.Links, Link{From: "t0", To: "t299", Mode: "after"})
	if _, e := validateProject(&p); e == nil {
		t.Fatal("1501 links accepted")
	}
	p = makeProject(301)
	if _, e := validateProject(&p); e == nil {
		t.Fatal("301 tasks accepted")
	}
	p = makeProject(33)
	for i := 1; i < len(p.Tasks); i++ {
		p.Tasks[i].Parent = p.Tasks[i-1].ID
	}
	if _, e := validateProject(&p); e != nil {
		t.Fatal("depth32 rejected", e)
	}
	p.Tasks = append(p.Tasks, Task{ID: "deep", Parent: "t32", Title: "Synthetic", Status: "todo", Responsibility: "internal"})
	if _, e := validateProject(&p); e == nil {
		t.Fatal("depth33 accepted")
	}
	p = makeProject(2)
	p.Tasks[0].Parent = "t1"
	p.Tasks[1].Parent = "t0"
	if _, e := validateProject(&p); e == nil {
		t.Fatal("hierarchy cycle accepted")
	}
	p = makeProject(2)
	p.Links = []Link{{From: "t0", To: "t1", Mode: "after"}, {From: "t1", To: "t0", Mode: "after"}}
	if _, e := validateProject(&p); e == nil {
		t.Fatal("dependency cycle accepted")
	}
	p = makeProject(3)
	p.Tasks[1].Parent = "t0"
	p.Tasks[2].Parent = "t0"
	p.Links = []Link{{From: "t0", To: "t1", Mode: "after"}}
	if _, e := validateProject(&p); e == nil {
		t.Fatal("container own descendant accepted")
	}
	p = makeProject(300)
	for i := 2; i < 151; i++ {
		p.Tasks[i].Parent = "t0"
	}
	for i := 151; i < 300; i++ {
		p.Tasks[i].Parent = "t1"
	}
	p.Links = []Link{{From: "t0", To: "t1", Mode: "after"}}
	expanded, e := validateProject(&p)
	if e != nil || len(expanded.order) != 298 {
		t.Fatal("bounded expanded container DAG failed")
	}
	p.Links = append(p.Links, Link{From: "t299", To: "t2", Mode: "after"})
	if _, e := validateProject(&p); e == nil {
		t.Fatal("expanded container reverse cycle accepted")
	}
	t.Log("PASS: 149x149 expanded container edges accepted, reverse cycle rejected")
	t.Log("PASS: max300tasks/1500links; rejection301/1501, depth33, hierarchy/dependency/expanded-container cycles; no unbounded stress")
}
