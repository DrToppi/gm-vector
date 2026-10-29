package planner

import (
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestDirectorySyncLifecycle(t *testing.T) {
	dsn := os.Getenv("GMA_TEST_DSN")
	if dsn == "" {
		t.Skip("GMA_TEST_DSN required")
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
	schema := "directory_" + newID()
	if _, e = root.DB.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer root.DB.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	q := parsed.Query()
	q.Set("search_path", schema)
	parsed.RawQuery = q.Encode()
	s, e := Open(ctx, parsed.String())
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	a := &App{Store: s}
	actor := newID()
	if _, e = s.DB.Exec(ctx, "INSERT INTO users(id,username,name,password_hash,totp,admin,active) VALUES($1,$1,'Admin','', '',true,true)", actor); e != nil {
		t.Fatal(e)
	}
	active := true
	b := directoryBatch{Provider: "synthetic", Entries: []DirectoryEntry{{ID: "stable-1", Name: "Synthetic User", Email: "user@example.test", Username: "corporate.user", Active: &active}}}
	if _, e = a.applyDirectory(ctx, b, actor, "synthetic"); e != nil {
		t.Fatal(e)
	}
	var eid string
	if e = s.DB.QueryRow(ctx, "SELECT id FROM employees WHERE external_id='stable-1'").Scan(&eid); e != nil {
		t.Fatal(e)
	}
	uid := newID()
	for _, sql := range []string{"INSERT INTO users(id,username,name,password_hash,totp,active) VALUES($1,$1,'Member','', '',true)", "INSERT INTO sessions(hash,user_id,csrf) VALUES('synthetic-session',$1,'csrf')", "INSERT INTO api_tokens(hash,user_id,name,project_ids,scopes,expires_at) VALUES('synthetic-token',$1,'test','{}','{}',now()+interval '1 hour')"} {
		if _, e = s.DB.Exec(ctx, sql, uid); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = s.DB.Exec(ctx, "UPDATE employees SET user_id=$1 WHERE id=$2", uid, eid); e != nil {
		t.Fatal(e)
	}
	out, e := a.applyDirectory(ctx, directoryBatch{Provider: "synthetic", Entries: []DirectoryEntry{}}, actor, "missing")
	if e != nil || out["missing"] != 1 {
		t.Fatalf("missing policy %v %v", out, e)
	}
	var enabled bool
	s.DB.QueryRow(ctx, "SELECT active FROM users WHERE id=$1", uid).Scan(&enabled)
	if !enabled {
		t.Fatal("missing identity disabled")
	}
	active = false
	if _, e = a.applyDirectory(ctx, b, actor, "disabled"); e != nil {
		t.Fatal(e)
	}
	s.DB.QueryRow(ctx, "SELECT active FROM users WHERE id=$1", uid).Scan(&enabled)
	if enabled {
		t.Fatal("explicit disabled identity remains active")
	}
	var credentials int
	s.DB.QueryRow(ctx, "SELECT (SELECT count(*) FROM sessions WHERE user_id=$1)+(SELECT count(*) FROM api_tokens WHERE user_id=$1)", uid).Scan(&credentials)
	if credentials != 0 {
		t.Fatal("credentials survived deactivation")
	}
	active = true
	if _, e = a.applyDirectory(ctx, b, actor, "returned"); e != nil {
		t.Fatal(e)
	}
	s.DB.QueryRow(ctx, "SELECT active FROM users WHERE id=$1", uid).Scan(&enabled)
	if enabled {
		t.Fatal("sync silently restored access")
	}
	var did string
	if e = s.DB.QueryRow(ctx, "SELECT id FROM directory_entries WHERE provider='synthetic' AND external_id='stable-1'").Scan(&did); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec(ctx, "UPDATE users SET auth_provider='ldap',directory_id=$2,active=true,totp=$3 WHERE id=$1", uid, did, []byte("synthetic-totp")); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec(ctx, "INSERT INTO sessions(hash,user_id,csrf) VALUES('fresh-session',$1,'csrf')", uid); e != nil {
		t.Fatal(e)
	}
	if e = a.revokeDirectoryAccess(ctx, uid, did); e != nil {
		t.Fatal(e)
	}
	if e = s.DB.QueryRow(ctx, "SELECT active FROM users WHERE id=$1", uid).Scan(&enabled); e != nil || enabled {
		t.Fatal("fresh disabled account not revoked", e)
	}
	if e = s.DB.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE user_id=$1", uid).Scan(&credentials); e != nil || credentials != 0 {
		t.Fatal("fresh disabled sessions retained", e)
	}
	cfg := fixtureLDAP()
	cfg.Provider = "synthetic"
	a.Config.LDAP = &cfg
	req := httptest.NewRequest("PATCH", "/api/v1/users/"+uid, strings.NewReader(`{"active":true}`))
	req.SetPathValue("id", uid)
	if e = a.updateUser(httptest.NewRecorder(), req, Principal{User: User{ID: actor, Admin: true}}); e == nil {
		t.Fatal("administrator reenabled inactive directory identity")
	}
	if _, e = s.DB.Exec(ctx, "INSERT INTO enrollments(hash,user_id,expires_at) VALUES('pending-disabled',$1,now()+interval '1 hour')", uid); e != nil {
		t.Fatal(e)
	}
	if e = validateEmployeeEnrollment(ctx, s.DB, "pending-disabled"); e == nil {
		t.Fatal("pending enrollment accepted inactive directory identity")
	}
	if e = a.revokeDirectoryAccess(ctx, actor, did); e == nil {
		t.Fatal("local administrator revocable through LDAP")
	}

}
