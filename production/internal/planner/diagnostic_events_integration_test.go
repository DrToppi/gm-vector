package planner

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticAPIIntegration(t *testing.T) {
	dsn := os.Getenv("GMA_TEST_DSN")
	if dsn == "" {
		t.Skip("set GMA_TEST_DSN for isolated PostgreSQL checks")
	}
	parsed, e := url.Parse(dsn)
	if e != nil || parsed.Path != "/gma_planner_test" {
		t.Fatal("refusing non-test database")
	}
	ctx := context.Background()
	root, e := Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer root.DB.Close()
	schema := "test_" + newID()
	if _, e = root.DB.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer root.DB.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	q := parsed.Query()
	q.Set("search_path", schema)
	parsed.RawQuery = q.Encode()
	store, e := Open(ctx, parsed.String())
	if e != nil {
		t.Fatal(e)
	}
	defer store.DB.Close()
	if e = store.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	a := &App{Store: store, Config: Config{Origin: "https://localhost:9443", WebDir: t.TempDir()}}
	for _, name := range []string{"alice", "bob", "admin"} {
		if _, e = store.DB.Exec(ctx, "INSERT INTO users(id,username,name,password_hash,totp,admin,active) VALUES($1,$1,$1,'unused',$2,$3,true)", name, []byte{}, name == "admin"); e != nil {
			t.Fatal(e)
		}
		if _, e = store.DB.Exec(ctx, "INSERT INTO sessions(hash,user_id,csrf) VALUES($1,$2,'csrf')", hash(name+"-cookie"), name); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = store.DB.Exec(ctx, "INSERT INTO api_tokens(hash,user_id,name,project_ids,scopes,expires_at) VALUES($1,'admin','test','{}',ARRAY['project:read'],now()+interval '1 hour')", hash("service-token")); e != nil {
		t.Fatal(e)
	}
	h := a.Handler()
	request := func(path, user, bearer string) (int, []byte) {
		r := httptest.NewRequest("GET", "https://localhost:9443/api/v1"+path, nil)
		if user != "" {
			r.Header.Set("Cookie", "__Host-gma="+user+"-cookie")
		}
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code, w.Body.Bytes()
	}
	for _, name := range []string{"alice", "bob"} {
		if code, _ := request("/projects/PRIVATE-"+name+"?secret=SECRET", name, ""); code != 404 && code != 403 {
			t.Fatalf("project: %d", code)
		}
	}
	if code, _ := request("/diagnostics/events", "", ""); code != 401 {
		t.Fatalf("anonymous: %d", code)
	}
	if code, _ := request("/diagnostics/events", "", "service-token"); code != 403 {
		t.Fatalf("token: %d", code)
	}
	for _, name := range []string{"alice", "bob", "admin"} {
		code, raw := request("/diagnostics/events", name, "")
		if code != 200 {
			t.Fatalf("%s: %d", name, code)
		}
		var body struct {
			Events []DiagnosticEvent `json:"events"`
		}
		if e = json.Unmarshal(raw, &body); e != nil {
			t.Fatal(e)
		}
		want := a.diagnosticEvents.snapshot(time.Now(), name, name == "admin")
		// The export request itself is appended after its response is encoded.
		if len(body.Events) != len(want)-1 {
			t.Fatalf("%s filtering %d != %d", name, len(body.Events), len(want)-1)
		}
		for i, event := range body.Events {
			if event.RequestID != want[i].RequestID {
				t.Fatalf("%s received another owner event", name)
			}
		}
		if name != "admin" && (len(body.Events) != 1 || body.Events[0].Route != "/api/v1/projects/{id}") {
			t.Fatalf("unexpected own events: %+v", body.Events)
		}
		for _, secret := range []string{"PRIVATE", "SECRET", "alice", "bob", "cookie", "owner", "service-token"} {
			if strings.Contains(string(raw), secret) {
				t.Fatal("leak", secret)
			}
		}
	}
}
