package planner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// Audit probes assert observed behavior, not desired fixes. Synthetic schemas only.
func TestSecurityAuditAccess(t *testing.T) {
	dsn := os.Getenv("GMA_TEST_DSN")
	if dsn == "" {
		t.Skip("requires explicitly isolated test DB")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Path != "/gma_planner_test" || parsed.Hostname() != "127.0.0.1" || parsed.Port() != "15432" {
		t.Fatal("refusing non-audit database")
	}
	ctx := context.Background()
	root, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal("test DB unavailable")
	}
	defer root.DB.Close()
	schema := "audit_" + newID()
	if _, err = root.DB.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer root.DB.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	q := parsed.Query()
	q.Set("search_path", schema)
	parsed.RawQuery = q.Encode()
	s, err := Open(ctx, parsed.String())
	if err != nil {
		t.Fatal("schema unavailable")
	}
	defer s.DB.Close()
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Origin: "https://audit.invalid", WebDir: t.TempDir(), Secret: random(32), Bootstrap: token()}
	a, err := NewApp(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("SEC-A01 shared proxy throttling", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			r := httptest.NewRequest("POST", "https://audit.invalid/api/v1/auth/login", nil)
			r.RemoteAddr = "192.0.2.1:1234"
			r.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i))
			if err = a.throttle(r, fmt.Sprintf("audit-%d", i)); err != nil {
				t.Fatal(i, err)
			}
		}
		r := httptest.NewRequest("POST", "https://audit.invalid/api/v1/auth/login", nil)
		r.RemoteAddr = "192.0.2.1:5678"
		r.Header.Set("X-Forwarded-For", "203.0.113.1")
		err = a.throttle(r, "unrelated-victim")
		ae, ok := err.(appError)
		if !ok || ae.Status != 429 {
			t.Fatal("expected shared 429", err)
		}
		t.Log("confirmed: 100 distinct synthetic accounts exhaust shared proxy bucket; unrelated victim gets 429")
	})
	uid := newID()
	if _, err = s.DB.Exec(ctx, "INSERT INTO users(id,username,name,password_hash,totp,admin,active) VALUES($1,'audit','Audit','', $2,true,true)", uid, []byte{}); err != nil {
		t.Fatal(err)
	}
	_, cal, _ := fixture()
	if err = a.SeedCalendar(ctx, cal); err != nil {
		t.Fatal(err)
	}
	pid := newID()
	if _, err = s.DB.Exec(ctx, "INSERT INTO projects(id,title,start_date,duration_mode,calendar_id) VALUES($1,'Audit','2026-10-28','calendar',$2)", pid, cal.ID); err != nil {
		t.Fatal(err)
	}
	t.Run("explicit administrator API token creation", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"name": "audit-token", "project_ids": []string{pid}, "scopes": []string{"project:write"}, "days": 1})
		r := httptest.NewRequest("POST", "https://audit.invalid/api/v1/tokens", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		u := Principal{}
		u.ID = uid
		u.Admin = true
		if err := a.createToken(w, r, u); err != nil {
			t.Fatal(err)
		}
		if w.Code != 201 {
			t.Fatal(w.Code)
		}
		t.Log("explicit administrator issues project-scoped token")
	})
	t.Run("all protected routes reject anonymous", func(t *testing.T) {
		source, err := os.ReadFile("app.go")
		if err != nil {
			t.Fatal(err)
		}
		routes := regexp.MustCompile(`"((?:GET|POST|PUT|PATCH|DELETE) /api/[^" ]+)":`).FindAllStringSubmatch(string(source), -1)
		for _, m := range routes {
			parts := strings.SplitN(m[1], " ", 2)
			path := strings.NewReplacer("{id}", pid, "{task}", "absent", "{entry}", "absent").Replace(parts[1])
			r := httptest.NewRequest(parts[0], cfg.Origin+path, strings.NewReader(`{}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", cfg.Origin)
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, r)
			if w.Code != 401 {
				t.Errorf("%s: got %d want401", m[1], w.Code)
			}
		}
		t.Logf("all %d protected method/path routes reject anonymous", len(routes))
	})
	t.Run("roles scope and revocation matrix", func(t *testing.T) {
		adminRoutes := []string{"GET /api/v1/admin/updates", "POST /api/v1/admin/updates/check", "POST /api/v1/admin/updates/apply", "POST /api/v1/admin/updates/recover", "GET /api/v1/users", "POST /api/v1/users", "PATCH /api/v1/users/absent", "POST /api/v1/employees", "POST /api/v1/employees/absent/invite", "GET /api/v1/employees/absent/invite", "POST /api/v1/calendars", "GET /api/v1/integrations", "GET /api/v1/diagnostics", "GET /api/v1/audit", "GET /api/v1/trash", "POST /api/v1/trash/absent/restore", "POST /api/v1/projects", "POST /api/v1/import", "GET /api/v1/tokens", "POST /api/v1/tokens", "DELETE /api/v1/tokens/absent", "GET /api/v1/directory/people?q=xx", "POST /api/v1/directory/sync", "GET /api/v1/directory/status", "POST /api/v1/directory/ldap/sync"}
		request := func(route, cookie, csrf, bearer string) int {
			parts := strings.SplitN(route, " ", 2)
			r := httptest.NewRequest(parts[0], cfg.Origin+parts[1], strings.NewReader(`{}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", cfg.Origin)
			if cookie != "" {
				r.Header.Set("Cookie", "__Host-gma="+cookie)
				r.Header.Set("X-CSRF-Token", csrf)
			}
			if bearer != "" {
				r.Header.Set("Authorization", "Bearer "+bearer)
			}
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, r)
			return w.Code
		}
		for _, edition := range []string{"community"} {
			for _, role := range []string{"outsider", "viewer", "member", "manager"} {
				user := newID()
				cookie, csrf := token(), token()
				if _, err = s.DB.Exec(ctx, "INSERT INTO users(id,username,name,password_hash,totp,active) VALUES($1,$1,'Synthetic','',$2,true)", user, []byte{}); err != nil {
					t.Fatal(err)
				}
				if _, err = s.DB.Exec(ctx, "INSERT INTO sessions(hash,user_id,csrf) VALUES($1,$2,$3)", hash(cookie), user, csrf); err != nil {
					t.Fatal(err)
				}
				if role != "outsider" {
					if _, err = s.DB.Exec(ctx, "INSERT INTO memberships VALUES($1,$2,$3)", pid, user, role); err != nil {
						t.Fatal(err)
					}
				}
				for _, route := range adminRoutes {
					if code := request(route, cookie, csrf, ""); code != 403 {
						t.Errorf("%s %s %s got%d want403", edition, role, route, code)
					}
				}
				read := request("GET /api/v1/projects/"+pid, cookie, csrf, "")
				want := 200
				if role == "outsider" {
					want = 404
				}
				if read != want {
					t.Errorf("%s %s project read %d want%d", edition, role, read, want)
				}
				for _, suffix := range []string{"members", "member-candidates", "export"} {
					want := 403
					if role == "manager" {
						want = 200
					}
					if role == "outsider" {
						want = 404
					}
					if code := request("GET /api/v1/projects/"+pid+"/"+suffix, cookie, csrf, ""); code != want {
						t.Errorf("%s %s %s %d want%d", edition, role, suffix, code, want)
					}
				}
				if _, err = s.DB.Exec(ctx, "UPDATE users SET active=false WHERE id=$1", user); err != nil {
					t.Fatal(err)
				}
				if code := request("GET /api/v1/auth/me", cookie, csrf, ""); code != 401 {
					t.Fatal("disabled account accepted", code)
				}
			}
		}
		for _, scope := range []string{"project:read", "project:write", "export"} {
			raw := token()
			if _, err = s.DB.Exec(ctx, "INSERT INTO api_tokens(hash,user_id,name,project_ids,scopes,expires_at) VALUES($1,$2,'Synthetic',$3,$4,now()+interval '1 hour')", hash(raw), uid, []string{pid}, []string{scope}); err != nil {
				t.Fatal(err)
			}
			want := 403
			if scope == "project:read" {
				want = 200
			}
			if code := request("GET /api/v1/projects/"+pid, "", "", raw); code != want {
				t.Errorf("scope%s read got%d", scope, code)
			}
			if code := request("GET /api/v1/users", "", "", raw); code != 403 {
				t.Fatal("token admin route", code)
			}
			if code := request("GET /api/v1/projects/foreign", "", "", raw); code != 403 && code != 404 {
				t.Fatal("token foreign project", code)
			}
			if _, err = s.DB.Exec(ctx, "DELETE FROM api_tokens WHERE hash=$1", hash(raw)); err != nil {
				t.Fatal(err)
			}
			if code := request("GET /api/v1/projects/"+pid, "", "", raw); code != 401 {
				t.Fatal("revoked token accepted", code)
			}
		}
		t.Log("role, admin, project permission, session and token revocation checks")
	})

	t.Run("session expiry CSRF and concurrent recovery", func(t *testing.T) {
		if _, err = s.DB.Exec(ctx, "DELETE FROM login_limits"); err != nil {
			t.Fatal(err)
		}
		secret := totpSecret()
		sealed, _ := seal(cfg.Secret, []byte(secret))
		recovery := token()[:20]
		user := newID()
		password := "synthetic audit password only"
		if _, err = s.DB.Exec(ctx, "INSERT INTO users(id,username,name,password_hash,totp,active,recovery_hashes) VALUES($1,$1,'Synthetic',$2,$3,true,$4)", user, passwordHash(password), sealed, []string{hash(recovery)}); err != nil {
			t.Fatal(err)
		}
		invoke := func(method, path, body, cookie, csrf, origin string) *httptest.ResponseRecorder {
			r := httptest.NewRequest(method, cfg.Origin+path, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", origin)
			r.Header.Set("X-CSRF-Token", csrf)
			if cookie != "" {
				r.Header.Set("Cookie", "__Host-gma="+cookie)
			}
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, r)
			return w
		}
		body, _ := json.Marshal(map[string]string{"username": user, "password": password, "code": recovery})
		responses := make(chan *httptest.ResponseRecorder, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				responses <- invoke("POST", "/api/v1/auth/login", string(body), "", "", cfg.Origin)
			}()
		}
		wg.Wait()
		close(responses)
		success, reject := 0, 0
		cookie := ""
		for w := range responses {
			switch w.Code {
			case 200:
				success++
				for _, c := range w.Result().Cookies() {
					if c.Name == "__Host-gma" {
						cookie = c.Value
						if !c.Secure || !c.HttpOnly || c.Path != "/" || c.SameSite != 3 {
							t.Fatal("unsafe cookie")
						}
					}
				}
			case 401:
				reject++
			default:
				t.Fatal("unexpected concurrent login", w.Code)
			}
		}
		if success != 1 || reject != 1 || cookie == "" {
			t.Fatal("recovery not one-time")
		}
		var csrf string
		if err = s.DB.QueryRow(ctx, "SELECT csrf FROM sessions WHERE hash=$1", hash(cookie)).Scan(&csrf); err != nil {
			t.Fatal(err)
		}
		if code := invoke("GET", "/api/v1/auth/me", "", cookie, "", cfg.Origin).Code; code != 200 {
			t.Fatal("session", code)
		}
		for _, v := range []struct{ csrf, origin string }{{"wrong", cfg.Origin}, {csrf, "https://foreign.invalid"}, {csrf, ""}} {
			if code := invoke("POST", "/api/v1/auth/logout", "{}", cookie, v.csrf, v.origin).Code; code != 403 {
				t.Fatal("CSRF bypass", code)
			}
		}
		if _, err = s.DB.Exec(ctx, "UPDATE sessions SET seen_at=now()-interval '31 minutes' WHERE hash=$1", hash(cookie)); err != nil {
			t.Fatal(err)
		}
		if code := invoke("GET", "/api/v1/auth/me", "", cookie, "", cfg.Origin).Code; code != 401 {
			t.Fatal("idle expiry", code)
		}
		if _, err = s.DB.Exec(ctx, "UPDATE sessions SET seen_at=now(),created_at=now()-interval '13 hours' WHERE hash=$1", hash(cookie)); err != nil {
			t.Fatal(err)
		}
		if code := invoke("GET", "/api/v1/auth/me", "", cookie, "", cfg.Origin).Code; code != 401 {
			t.Fatal("absolute expiry", code)
		}
		t.Log("recovery concurrent replay: exactly one success; cookie flags; invalid CSRF/foreign or missing Origin; idle and absolute expiry confirmed")
	})

}

func TestSecurityAuditMatrix(t *testing.T) {
	dsn := os.Getenv("GMA_TEST_DSN")
	if dsn == "" {
		t.Skip("requires explicit isolated DB")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Hostname() != "127.0.0.1" || parsed.Port() != "15432" || parsed.Path != "/gma_planner_test" {
		t.Fatal("refusing non-test DB")
	}
	ctx := context.Background()
	root, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal("test DB unavailable")
	}
	defer root.DB.Close()
	schema := "matrix_" + newID()
	if _, err = root.DB.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer root.DB.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	q := parsed.Query()
	q.Set("search_path", schema)
	parsed.RawQuery = q.Encode()
	s, err := Open(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Origin: "https://audit.invalid", WebDir: t.TempDir(), Secret: random(32), Bootstrap: token()}
	a, err := NewApp(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(cfg.WebDir+"/openapi.json", []byte(`{"openapi":"3.1.2"}`), 0600); err != nil {
		t.Fatal(err)
	}
	source, _ := os.ReadFile("app.go")
	matches := regexp.MustCompile(`"((?:GET|POST|PUT|PATCH|DELETE) /api/[^" ]+)":\s*a\.(\w+)`).FindAllStringSubmatch(string(source), -1)
	publicMatches := regexp.MustCompile(`mux.HandleFunc\("((?:GET|POST) /api/[^" ]+)", a.wrap\(a\.(\w+), false\)\)`).FindAllStringSubmatch(string(source), -1)
	matches = append(matches, publicMatches...)
	if len(matches) != 55 {
		t.Fatal("route inventory changed", len(matches))
	}
	password := "synthetic audit password only"
	ph := passwordHash(password)
	secret := totpSecret()
	sealed, _ := seal(cfg.Secret, []byte(secret))
	cookie, csrf, bearer, enroll := token(), token(), token(), token()
	results := []string{"endpoint,handler,role,variant,expected_http,actual_http,status"}
	failures := 0
	adminHandlers := map[string]bool{}
	for _, h := range strings.Fields("updaterStatus updaterCheck updaterApply updaterRecover users inviteUser updateUser saveEmployee inviteEmployee employeeInvite importCalendar integrations diagnostics auditLog trash restoreProject createProject importProject tokens createToken revokeToken directoryPeople directoryStatus syncDirectory syncLDAPDirectory") {
		adminHandlers[h] = true
	}
	managerOnly := map[string]bool{}
	for _, h := range strings.Fields("members setMember archiveProject undoProject exportProject memberCandidates") {
		managerOnly[h] = true
	}
	writerOnly := map[string]bool{}
	for _, h := range strings.Fields("saveProject previewProject addTimeEntry deleteTimeEntry addTaskComment") {
		writerOnly[h] = true
	}
	public := map[string]bool{}
	for _, m := range publicMatches {
		public[m[2]] = true
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := s.DB.Exec(ctx, sql, args...); err != nil {
			t.Fatal("synthetic fixture SQL failed", err)
		}
	}
	for _, state := range []string{"community"} {
		for _, role := range []string{"anonymous", "authenticated-no-membership", "viewer", "member", "manager", "administrator", "API-token"} {
			for _, route := range matches {
				pattern, h := route[1], route[2]
				isAdmin := role == "administrator" || role == "API-token"
				exec("TRUNCATE users,employees,calendars,projects,settings,login_limits,audit,outbox,event_mappings CASCADE")
				exec("INSERT INTO users(id,username,name,password_hash,totp,active,admin) VALUES('actor','actor','Synthetic',$1,$2,true,$3),('target','target','Target',$1,$2,false,false)", ph, sealed, isAdmin)
				exec("INSERT INTO employees(id,name,daily_minutes,user_id) VALUES('e','Employee',480,'actor')")
				project, cal, _ := fixture()
				project.Revision = 1
				if err = a.SeedCalendar(ctx, cal); err != nil {
					t.Fatal(err)
				}
				exec("INSERT INTO projects(id,title,start_date,duration_mode,calendar_id,revision) VALUES('p','Test','2026-10-23','working',$1,1),('archived','Archive','2026-10-23','working',$1,1)", cal.ID)
				exec("UPDATE projects SET archived_at=now() WHERE id='archived'")
				tx, err := s.DB.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if err = writeProject(ctx, tx, project); err != nil {
					t.Fatal(err)
				}
				if err = tx.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				if role == "viewer" || role == "member" || role == "manager" {
					exec("INSERT INTO memberships VALUES('p','actor',$1)", role)
				}
				exec("INSERT INTO time_entries VALUES('entry','p','a','e','2026-10-23',5,'Synthetic','actor')")
				exec("INSERT INTO sessions(hash,user_id,csrf) VALUES($1,'actor',$2)", hash(cookie), csrf)
				exec("INSERT INTO api_tokens(hash,user_id,name,project_ids,scopes,expires_at) VALUES($1,'actor','Synthetic',ARRAY['p'],ARRAY['project:read','project:write','export'],now()+interval '1 hour')", hash(bearer))
				// Synthetic invited target supports valid enrollment/resume/confirm bodies.
				pending, _ := json.Marshal(enrollment{ID: "target", Username: "target", Name: "Target", PasswordHash: ph, Secret: secret})
				pendingSealed, _ := seal(cfg.Secret, pending)
				exec("INSERT INTO enrollments(hash,user_id,expires_at,payload) VALUES($1,'target',now()+interval '1 hour',$2)", hash(enroll), pendingSealed)
				project, err = readProject(ctx, s.DB, "p")
				if err != nil {
					t.Fatal(err)
				}
				if err = s.prepare(ctx, s.DB, &project); err != nil {
					t.Fatal(err)
				}
				snapshot := project
				snapshot.Revision = 0
				raw, _ := json.Marshal(snapshot)
				exec("INSERT INTO snapshots(project_id,revision,payload) VALUES('p',0,$1)", raw)
				var body any = map[string]any{}
				path := strings.NewReplacer("{id}", "p", "{task}", "a", "{entry}", "entry").Replace(strings.SplitN(pattern, " ", 2)[1])
				method := strings.SplitN(pattern, " ", 2)[0]
				want := 200
				switch h {
				case "updaterCheck":
					body = map[string]string{"ref": "main"}
					want = 503
				case "updaterApply":
					body = map[string]string{"commit": strings.Repeat("a", 40), "confirmation": strings.Repeat("a", 40)}
					want = 503
				case "updaterRecover":
					body = map[string]string{"operation_id": strings.Repeat("b", 32), "confirmation": strings.Repeat("b", 32)}
					want = 503
				case "createProject":
					body = map[string]string{"title": "New", "start": "2026-10-23", "duration_mode": "working", "calendar_id": cal.ID}
					want = 201
				case "saveProject", "previewProject":
					project.Tasks[0].Brief = "Synthetic edit"
					body = saveRequest{Project: project}
				case "archiveProject", "undoProject":
					body = map[string]int{"revision": 1}
				case "restoreProject":
					path = "/api/v1/trash/archived/restore"
					body = map[string]int{"revision": 1}
				case "setMember":
					body = map[string]string{"user_id": "target", "role": "viewer"}
				case "addTimeEntry":
					body = TimeEntry{TaskID: "a", EmployeeID: "e", Date: "2026-10-23", Minutes: 10, Comment: "Synthetic"}
					want = 201
				case "addTaskComment":
					body = map[string]any{"id": "12345678-1234-4234-8234-123456789abc", "text": "Synthetic", "send_to_customer": false}
					want = 201
				case "inviteUser":
					body = map[string]any{"username": "new-user", "name": "New", "admin": false}
					want = 201
				case "updateUser":
					path = "/api/v1/users/target"
					body = map[string]bool{"active": true}
				case "saveEmployee":
					body = Employee{ID: "newemployee", Name: "New", Daily: 480}
				case "employeeInvite":
					path = "/api/v1/employees/e/invite"
				case "inviteEmployee":
					path = "/api/v1/employees/e/invite"
					body = map[string]any{"id": "12345678-1234-4234-8234-123456789abc", "reissue": false}
					want = 503
				case "importCalendar":
					cal.ID = "imported"
					body = cal
					want = 201
				case "createToken":
					body = map[string]any{"name": "New", "project_ids": []string{"p"}, "scopes": []string{"project:write"}, "days": 1}
					want = 201
				case "revokeToken":
					path = "/api/v1/tokens/" + hash(bearer)
				case "importProject":
					imp := project
					imp.People = []Participation{}
					for i := range imp.Tasks {
						imp.Tasks[i].Assignments = []Assignment{}
					}
					body = ImportRequest{Project: imp}
					want = 201
				case "directoryPeople", "directoryStatus":
					want = 200
				case "syncDirectory":
					body = map[string]any{"provider": "fixture", "entries": []any{}}
				case "syncLDAPDirectory":
					want = 422
				case "preferences":
					body = map[string]string{"locale": "en"}
				case "saveBugReport":
					want = 503
				case "login":
					body = map[string]string{"username": "actor", "password": password, "code": totpAt(secret, time.Now().Unix()/30)}
				case "changePassword":
					body = map[string]string{"old_password": password, "new_password": "another synthetic password", "code": totpAt(secret, time.Now().Unix()/30)}
				case "beginEnrollment":
					body = map[string]string{"token": enroll, "username": "target", "name": "Target", "password": password}
				case "resumeEnrollment":
					body = map[string]string{"token": enroll}
				case "confirmEnrollment":
					body = map[string]string{"token": enroll, "code": totpAt(secret, time.Now().Unix()/30)}

				}
				if !public[h] {
					if role == "anonymous" {
						want = 401
					} else if role == "API-token" {
						if !strings.HasPrefix(path, "/api/v1/projects") || h == "members" || h == "memberCandidates" || h == "setMember" || h == "createProject" {
							want = 403
						}
					} else if adminHandlers[h] && !isAdmin {
						want = 403
					} else if strings.Contains(pattern, "/projects/{id}") {
						if role == "authenticated-no-membership" {
							want = 404
						} else if managerOnly[h] && role != "manager" && !isAdmin {
							want = 403
						} else if writerOnly[h] && role == "viewer" {
							want = 403
						}
					}
				}
				raw, _ = json.Marshal(body)
				r := httptest.NewRequest(method, cfg.Origin+path, bytes.NewReader(raw))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Origin", cfg.Origin)
				if role == "API-token" {
					r.Header.Set("Authorization", "Bearer "+bearer)
				} else if role != "anonymous" {
					r.Header.Set("Cookie", "__Host-gma="+cookie)
					r.Header.Set("X-CSRF-Token", csrf)
				}
				w := httptest.NewRecorder()
				a.Handler().ServeHTTP(w, r)
				status := "checked_problem_not_reproduced"
				if w.Code != want {
					status = "unexpected_requires_review"
					failures++
					t.Errorf("%s %s %s got%d expected%d", state, role, pattern, w.Code, want)
				}
				results = append(results, fmt.Sprintf("%s,%s,%s,%s,%d,%d,%s", pattern, h, role, state, want, w.Code, status))
			}
		}
	}
	if out := os.Getenv("GMA_AUDIT_MATRIX_OUTPUT"); out != "" {
		if err = os.WriteFile(out, []byte(strings.Join(results, "\n")+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("matrix cells %d; unexpected results %d; separate synthetic fixture per request", len(results)-1, failures)
}

func TestSecurityAuditRevocationSeatRace(t *testing.T) {
	dsn := os.Getenv("GMA_TEST_DSN")
	if dsn == "" {
		t.Skip("requires explicit isolated DB")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Hostname() != "127.0.0.1" || parsed.Port() != "15432" || parsed.Path != "/gma_planner_test" {
		t.Fatal("refusing non-test DB")
	}
	ctx := context.Background()
	root, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal("test DB unavailable")
	}
	defer root.DB.Close()
	schema := "race_" + newID()
	if _, err = root.DB.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer root.DB.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	q := parsed.Query()
	q.Set("search_path", schema)
	q.Set("application_name", schema)
	parsed.RawQuery = q.Encode()
	s, err := Open(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Origin: "https://audit.invalid", WebDir: t.TempDir(), Secret: random(32), Bootstrap: token()}
	a, err := NewApp(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := s.DB.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO users(id,username,name,password_hash,totp,active,admin) VALUES('admin','admin','Admin','',$1,true,true),('manager','manager','Manager','',$1,true,false)", []byte{})
	_, cal, _ := fixture()
	if err = a.SeedCalendar(ctx, cal); err != nil {
		t.Fatal(err)
	}
	exec("INSERT INTO projects(id,title,start_date,duration_mode,calendar_id) VALUES('p','Original','2026-10-23','working',$1)", cal.ID)
	exec("INSERT INTO memberships VALUES('p','manager','manager')")

	cookie, csrf, bearer := token(), token(), token()
	exec("INSERT INTO sessions(hash,user_id,csrf) VALUES($1,'manager',$2)", hash(cookie), csrf)
	exec("INSERT INTO api_tokens(hash,user_id,name,project_ids,scopes,expires_at) VALUES($1,'admin','Synthetic',ARRAY['p'],ARRAY['project:write'],now()+interval '1 hour')", hash(bearer))
	invoke := func(path string, body any, useToken bool) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest("PUT", cfg.Origin+path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", cfg.Origin)
		if useToken {
			r.Header.Set("Authorization", "Bearer "+bearer)
		} else {
			r.Header.Set("Cookie", "__Host-gma="+cookie)
			r.Header.Set("X-CSRF-Token", csrf)
		}
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		return w
	}
	for _, kind := range []string{"membership", "token"} {
		t.Run(kind, func(t *testing.T) {
			project, err := readProject(ctx, s.DB, "p")
			if err != nil {
				t.Fatal(err)
			}
			project.Title = "Changed after " + kind + " revocation"
			locker, err := s.DB.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer locker.Rollback(ctx)
			if _, err = locker.Exec(ctx, "SELECT id FROM projects WHERE id='p' FOR UPDATE"); err != nil {
				t.Fatal(err)
			}
			responses := make(chan *httptest.ResponseRecorder, 1)
			go func() { responses <- invoke("/api/v1/projects/p", saveRequest{Project: project}, kind == "token") }()
			waiting := false
			for i := 0; i < 200; i++ {
				if err = s.DB.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock' AND query LIKE 'SELECT revision FROM projects%')", schema).Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !waiting {
				t.Fatal("request failed to reach controlled lock barrier")
			}
			if kind == "membership" {
				if _, err = locker.Exec(ctx, "UPDATE memberships SET role='viewer' WHERE user_id='manager'"); err != nil {
					t.Fatal(err)
				}
			} else {
				exec("DELETE FROM api_tokens WHERE hash=$1", hash(bearer))
			}
			if err = locker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			w := <-responses
			if kind == "membership" {
				if w.Code != 403 {
					t.Fatalf("membership revoke bypass: %d", w.Code)
				}
			} else {
				if w.Code != 200 {
					t.Fatalf("unexpected in-flight token semantics: %d", w.Code)
				}
				if w2 := invoke("/api/v1/projects/p", saveRequest{Project: project}, true); w2.Code != 401 {
					t.Fatal("subsequent revoked token accepted", w2.Code)
				}
				t.Log("in-flight authorized request completes after token revocation; subsequent request gets401")
			}
		})
	}
}
