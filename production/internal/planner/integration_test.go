package planner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type testSession struct{ cookie, csrf string }

func TestAPIIntegration(t *testing.T) {
	dsn := os.Getenv("GMA_TEST_DSN")
	if dsn == "" {
		t.Skip("set GMA_TEST_DSN for isolated PostgreSQL integration checks")
	}
	parsed, e := url.Parse(dsn)
	if e != nil || parsed.Path != "/gma_planner_test" {
		t.Fatal("refusing a non-test database")
	}
	ctx := context.Background()
	root, e := Open(ctx, dsn)
	if e != nil {
		t.Fatal("test database unavailable", e)
	}
	defer root.DB.Close()
	dbSchema := "test_" + newID()
	if _, e = root.DB.Exec(ctx, "CREATE SCHEMA "+dbSchema); e != nil {
		t.Fatal(e)
	}
	defer root.DB.Exec(ctx, "DROP SCHEMA "+dbSchema+" CASCADE")
	q := parsed.Query()
	q.Set("search_path", dbSchema)
	parsed.RawQuery = q.Encode()
	store, e := Open(ctx, parsed.String())
	if e != nil {
		t.Fatal(e)
	}
	defer store.DB.Close()
	if e = store.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	cfg := Config{Origin: "https://localhost:9443", WebDir: t.TempDir(), Secret: random(32)}
	app, e := NewApp(store, cfg)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewTLSServer(app.Handler())
	defer server.Close()
	client := server.Client()
	request := func(method, path string, body any, session *testSession, csrfOverride *string) (int, json.RawMessage) {
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		req, e := http.NewRequest(method, server.URL+"/api/v1"+path, bytes.NewReader(raw))
		if e != nil {
			t.Fatal(e)
		}
		req.Host = "localhost:9443"
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", cfg.Origin)
		if session != nil {
			req.Header.Set("Cookie", "__Host-gma="+session.cookie)
			req.Header.Set("X-CSRF-Token", session.csrf)
		}
		if csrfOverride != nil {
			req.Header.Set("X-CSRF-Token", *csrfOverride)
		}
		res, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		b, e := io.ReadAll(res.Body)
		if e != nil {
			t.Fatal(e)
		}
		for _, c := range res.Cookies() {
			if session != nil && c.Name == "__Host-gma" {
				session.cookie = c.Value
			}
		}
		return res.StatusCode, b
	}
	expect := func(want, got int, where string) {
		t.Helper()
		if want != got {
			t.Fatalf("%s: status %d, want %d", where, got, want)
		}
	}
	_, cal, _ := fixture()
	if e = app.SeedCalendar(ctx, cal); e != nil {
		t.Fatal(e)
	}
	admin := testSession{}
	code, _ := request("POST", "/auth/bootstrap", map[string]any{"token": token(), "username": "admin", "name": "Admin", "password": "correct horse battery staple"}, nil, nil)
	expect(403, code, "wrong bootstrap")
	code, b := request("POST", "/auth/bootstrap", map[string]any{"username": "admin", "name": "Admin", "password": "correct horse battery staple"}, nil, nil)
	expect(200, code, "bootstrap")
	var enroll struct {
		Secret string `json:"secret"`
		Token  string `json:"enrollment_token"`
	}
	json.Unmarshal(b, &enroll)
	if len(enroll.Token) < 32 {
		t.Fatal("missing opaque continuation token")
	}
	code, otherBody := request("POST", "/auth/bootstrap", map[string]any{"username": "other-admin", "name": "Other", "password": "another correct horse battery staple"}, nil, nil)
	expect(200, code, "independent unfinished first-run")
	var other struct {
		Secret string `json:"secret"`
		Token  string `json:"enrollment_token"`
	}
	json.Unmarshal(otherBody, &other)
	if other.Token == enroll.Token || other.Secret == enroll.Secret {
		t.Fatal("first-run attempts share credentials")
	}
	code, _ = request("POST", "/auth/bootstrap/confirm", map[string]string{"code": totpAt(enroll.Secret, time.Now().Unix()/30)}, nil, nil)
	expect(403, code, "confirmation requires browser continuation")
	code, b = request("POST", "/auth/bootstrap/resume", map[string]string{"token": enroll.Token}, nil, nil)
	expect(200, code, "resume unfinished enrollment")
	var resumed struct {
		Secret string `json:"secret"`
	}
	json.Unmarshal(b, &resumed)
	if resumed.Secret != enroll.Secret {
		t.Fatal("resuming rotated MFA secret")
	}
	code, _ = request("POST", "/auth/bootstrap/resume", map[string]string{"token": token()}, nil, nil)
	expect(403, code, "foreign enrollment denied")
	otp := totpAt(enroll.Secret, time.Now().Unix()/30)
	code, b = request("POST", "/auth/bootstrap/confirm", map[string]any{"token": enroll.Token, "code": otp}, &admin, nil)
	expect(200, code, "MFA confirmation")
	code, _ = request("POST", "/auth/bootstrap/resume", map[string]string{"token": enroll.Token}, nil, nil)
	expect(403, code, "consumed key cannot resume")
	code, _ = request("POST", "/auth/bootstrap/resume", map[string]string{"token": other.Token}, nil, nil)
	expect(403, code, "losing first-run cannot resume")
	code, _ = request("POST", "/auth/bootstrap/confirm", map[string]string{"token": other.Token, "code": totpAt(other.Secret, time.Now().Unix()/30)}, nil, nil)
	expect(403, code, "losing first-run cannot create second administrator")
	code, _ = request("POST", "/auth/bootstrap", map[string]string{"username": "replacement", "name": "Replacement", "password": "correct horse battery staple"}, nil, nil)
	expect(403, code, "setup closed without token after confirmation")
	var recovery struct {
		Codes []string `json:"recovery_codes"`
	}
	json.Unmarshal(b, &recovery)
	if len(recovery.Codes) != 10 {
		t.Fatal("missing recovery codes")
	}
	code, b = request("GET", "/auth/me", nil, &admin, nil)
	expect(200, code, "me")
	var me struct {
		User User   `json:"user"`
		CSRF string `json:"csrf"`
	}
	json.Unmarshal(b, &me)
	admin.csrf = me.CSRF
	t.Run("updater request audit", func(t *testing.T) { testUpdaterAudit(t, app, me.User) })
	code, _ = request("PUT", "/auth/preferences", map[string]string{"locale": "en"}, &admin, nil)
	expect(200, code, "locale saved")
	code, b = request("GET", "/auth/me", nil, &admin, nil)
	expect(200, code, "profile locale")
	if !strings.Contains(string(b), `"locale":"en"`) {
		t.Fatal("locale not persisted")
	}
	code, _ = request("PUT", "/auth/preferences", map[string]string{"locale": "unsupported"}, &admin, nil)
	expect(422, code, "unsupported locale rejected")
	code, _ = request("POST", "/auth/login", map[string]string{"username": "admin", "password": "correct horse battery staple", "code": otp}, nil, nil)
	expect(401, code, "TOTP replay")
	code, _ = request("GET", "/projects", nil, nil, nil)
	expect(401, code, "anonymous projects")
	payload := map[string]string{"title": "Integration project", "start": "2026-10-23", "duration_mode": "working", "calendar_id": cal.ID}
	empty := ""
	code, _ = request("POST", "/projects", payload, &admin, &empty)
	expect(403, code, "CSRF")
	code, b = request("POST", "/projects", payload, &admin, nil)
	expect(201, code, "create project")
	var p Project
	json.Unmarshal(b, &p)
	if p.ID == "" {
		t.Fatal("no project ID")
	}
	code, b = request("POST", "/employees", Employee{ID: "e", Name: "Employee", Daily: 480, UserID: me.User.ID}, &admin, nil)
	expect(200, code, "employee")
	base, _, _ := fixture()
	p.Tasks = base.Tasks
	p.People = base.People
	p.Links = []Link{}
	code, b = request("PUT", "/projects/"+p.ID, saveRequest{Project: p}, &admin, nil)
	expect(200, code, "plan save")
	var saved Project
	json.Unmarshal(b, &saved)
	if saved.Revision != 1 || saved.Tasks[0].End != "2026-10-27" {
		t.Fatalf("bad calculation/revision: rev %d, end %s", saved.Revision, saved.Tasks[0].End)
	}
	code, _ = request("PUT", "/projects/"+p.ID, saveRequest{Project: p}, &admin, nil)
	expect(409, code, "stale revision")
	t.Run("atomic concurrent edits", func(t *testing.T) {
		var wg sync.WaitGroup
		statuses := make(chan int, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s, _ := request("PUT", "/projects/"+saved.ID, saveRequest{Project: saved}, &admin, nil)
				statuses <- s
			}()
		}
		wg.Wait()
		close(statuses)
		success, conflicts := 0, 0
		for s := range statuses {
			if s == 200 {
				success++
			}
			if s == 409 {
				conflicts++
			}
		}
		if success != 1 || conflicts != 1 {
			t.Fatalf("success=%d conflicts=%d", success, conflicts)
		}
	})
	code, b = request("GET", "/projects/"+p.ID, nil, &admin, nil)
	expect(200, code, "reload")
	var envelope struct {
		Project Project `json:"project"`
	}
	json.Unmarshal(b, &envelope)
	saved = envelope.Project
	t.Run("project start shift preview save undo", func(t *testing.T) {
		decodeProject := func(body json.RawMessage) Project {
			t.Helper()
			var value Project
			if err := json.Unmarshal(body, &value); err != nil || value.ID == "" {
				t.Fatalf("invalid project response: %v", err)
			}
			return value
		}
		get := func(id string) Project {
			t.Helper()
			status, body := request("GET", "/projects/"+id, nil, &admin, nil)
			if status != 200 {
				t.Fatalf("project reload: status %d", status)
			}
			var value struct{ Project Project }
			if err := json.Unmarshal(body, &value); err != nil || value.Project.ID != id {
				t.Fatalf("invalid project envelope: %v", err)
			}
			return value.Project
		}
		write := func(method, suffix string, value Project) Project {
			t.Helper()
			status, body := request(method, "/projects/"+value.ID+suffix, saveRequest{Project: value}, &admin, nil)
			if status != 200 {
				t.Fatalf("%s %s: status %d", method, suffix, status)
			}
			return decodeProject(body)
		}
		create := func() Project {
			t.Helper()
			status, body := request("POST", "/projects", payload, &admin, nil)
			if status != 201 {
				t.Fatalf("shift fixture creation: status %d", status)
			}
			value := decodeProject(body)
			fixture, _, _ := startShiftFixture(t)
			value.Tasks, value.People = fixture.Tasks, fixture.People
			for i := range value.Tasks {
				if value.Tasks[i].ID == "done" {
					value.Tasks[i].Assignments = []Assignment{{EmployeeID: "e", Daily: 240}}
				}
			}
			value.Tasks = append(value.Tasks, Task{ID: "next", Title: "Dependent", Status: "todo", Responsibility: "internal", RequestedStart: value.Start, DurationDays: 1})
			value.Links = []Link{{From: "a", To: "next", Mode: "after"}}
			value = write("PUT", "", value)
			for _, entry := range []TimeEntry{
				{TaskID: "a", EmployeeID: "e", Date: "2026-10-23", Minutes: 45, Comment: "Synthetic shift fixture"},
				{TaskID: "done", EmployeeID: "e", Date: "2026-10-20", Minutes: 150, Comment: "Synthetic finished fixture"},
			} {
				status, _ := request("POST", "/projects/"+value.ID+"/time-entries", entry, &admin, nil)
				if status != 201 {
					t.Fatalf("shift time entry fixture: status %d", status)
				}
			}
			return get(value.ID)
		}
		assertShift := func(value Project) {
			t.Helper()
			active, done, customer, summary, next := startTask(t, value, "a"), startTask(t, value, "done"), startTask(t, value, "customer"), startTask(t, value, "group"), startTask(t, value, "next")
			if value.Start != "2026-10-30" || active.RequestedStart != "2026-10-30" || active.Start != "2026-10-30" || active.End != "2026-11-03" || active.Planned != 600 || active.Actual != 45 || active.Status != "doing" {
				t.Fatalf("active task not shifted exactly once: %+v", active)
			}
			if done.Start != "2026-10-27" || done.End != "2026-10-28" || done.Actual != 150 || done.Status != "done" || done.LegacyStart != "2026-09-10" || done.LegacyEnd != "2026-09-11" {
				t.Fatalf("completed range/actual/legacy changed incorrectly: %+v", done)
			}
			if customer.Start != "2026-10-30" || customer.End != "2026-11-02" || customer.CustomerDue != "2026-11-30" {
				t.Fatalf("customer dates changed incorrectly: %+v", customer)
			}
			if !summary.Summary || summary.Start != "2026-10-27" || summary.End != "2026-11-03" || summary.Actual != 195 || next.Start != "2026-11-04" {
				t.Fatalf("summary/dependency not recalculated: summary=%+v next=%+v", summary, next)
			}
		}
		old := create()
		changed := copyStartProject(t, old)
		changed.Start = "2026-10-30"
		preview := write("POST", "/preview", changed)
		assertShift(preview)
		previewAgain := write("POST", "/preview", preview)
		assertShift(previewAgain)
		if previewAgain.Revision != old.Revision || !reflect.DeepEqual(previewAgain.Tasks, preview.Tasks) {
			t.Fatal("preview changed revision or shifted dates twice")
		}
		beforeCommit := get(old.ID)
		if beforeCommit.Start != old.Start || beforeCommit.Revision != old.Revision || !reflect.DeepEqual(beforeCommit.Tasks, old.Tasks) {
			t.Fatal("preview persisted changes")
		}
		committed := write("PUT", "", previewAgain)
		assertShift(committed)
		if committed.Revision != old.Revision+1 {
			t.Fatal("commit did not increment revision exactly once")
		}
		for i := 0; i < 2; i++ {
			reloaded := get(old.ID)
			assertShift(reloaded)
			if reloaded.Revision != committed.Revision {
				t.Fatal("GET changed the revision")
			}
		}
		status, body := request("GET", "/projects/"+old.ID+"/time-entries", nil, &admin, nil)
		var entries []TimeEntry
		if status != 200 || json.Unmarshal(body, &entries) != nil || len(entries) != 2 {
			t.Fatal("time entries missing after project shift")
		}
		for _, entry := range entries {
			if entry.TaskID == "a" && (entry.Date != "2026-10-23" || entry.Minutes != 45) || entry.TaskID == "done" && (entry.Date != "2026-10-20" || entry.Minutes != 150) {
				t.Fatalf("historical time entry moved: %+v", entry)
			}
		}
		for _, operation := range []struct{ method, suffix string }{{"POST", "/preview"}, {"PUT", ""}} {
			status, _ := request(operation.method, "/projects/"+old.ID+operation.suffix, saveRequest{Project: changed}, &admin, nil)
			if status != 409 {
				t.Fatalf("stale shift %s accepted: %d", operation.method, status)
			}
		}
		if current := get(old.ID); current.Revision != committed.Revision || !reflect.DeepEqual(current.Tasks, committed.Tasks) {
			t.Fatal("rejected stale request changed the project")
		}
		direct := create()
		direct.Start = "2026-10-30"
		direct = write("PUT", "", direct)
		assertShift(direct)
		if !reflect.DeepEqual(direct.Tasks, committed.Tasks) {
			t.Fatal("direct PUT differs from preview followed by PUT")
		}
		status, body = request("POST", "/projects/"+old.ID+"/undo", map[string]int64{"revision": committed.Revision}, &admin, nil)
		if status != 200 {
			t.Fatalf("undo shift: status %d", status)
		}
		undone := decodeProject(body)
		if undone.Start != old.Start || undone.Revision != committed.Revision+1 || !reflect.DeepEqual(undone.Tasks, old.Tasks) {
			t.Fatal("undo did not restore absolute snapshot dates and preserved effort")
		}
		if got := get(old.ID); got.Start != old.Start || !reflect.DeepEqual(got.Tasks, old.Tasks) {
			t.Fatal("GET after undo recalculated a second shift")
		}
	})
	nonadmin := newID()
	secret, _ := seal(cfg.Secret, []byte(totpSecret()))
	if _, e = store.DB.Exec(ctx, "INSERT INTO users(id,username,name,password_hash,totp,active) VALUES($1,$2,'Member','unused',$3,true)", nonadmin, "member-"+nonadmin, secret); e != nil {
		t.Fatal(e)
	}
	member := testSession{token(), token()}
	if _, e = store.DB.Exec(ctx, "INSERT INTO sessions(hash,user_id,csrf) VALUES($1,$2,$3)", hash(member.cookie), nonadmin, member.csrf); e != nil {
		t.Fatal(e)
	}
	code, _ = request("GET", "/projects/"+p.ID, nil, &member, nil)
	expect(404, code, "foreign project hidden")
	code, _ = request("GET", "/users", nil, &member, nil)
	expect(403, code, "user directory protected")
	if _, e = store.DB.Exec(ctx, "INSERT INTO memberships VALUES($1,$2,'viewer')", p.ID, nonadmin); e != nil {
		t.Fatal(e)
	}
	code, _ = request("GET", "/projects/"+p.ID, nil, &member, nil)
	expect(200, code, "viewer read")
	code, _ = request("PUT", "/projects/"+p.ID, saveRequest{Project: saved}, &member, nil)
	expect(403, code, "viewer write denied")
	code, _ = request("GET", "/projects/"+p.ID+"/export", nil, &member, nil)
	expect(403, code, "viewer export denied")
	bad := saved
	bad.Tasks = append([]Task(nil), saved.Tasks...)
	btask := bad.Tasks[0]
	btask.ID = "b"
	bad.Tasks = append(bad.Tasks, btask)
	bad.Links = []Link{{"a", "b", "after", 0}, {"b", "a", "after", 0}}
	code, _ = request("POST", "/projects/"+p.ID+"/preview", saveRequest{Project: bad}, &admin, nil)
	expect(422, code, "cycle rejected")

	code, _ = request("GET", "/projects/"+p.ID+"/member-candidates", nil, &member, nil)
	expect(403, code, "viewer cannot enumerate candidates")
	code, _ = request("GET", "/projects/"+p.ID+"/member-candidates", nil, &admin, nil)
	expect(200, code, "manager candidates")
	code, _ = request("DELETE", "/projects/"+p.ID, map[string]int64{"revision": saved.Revision}, &admin, nil)
	expect(200, code, "archive")
	code, _ = request("GET", "/projects/"+p.ID, nil, &admin, nil)
	expect(404, code, "archived hidden")
	code, _ = request("POST", "/trash/"+p.ID+"/restore", map[string]int64{"revision": saved.Revision + 1}, &member, nil)
	expect(403, code, "viewer restore denied")
	code, _ = request("POST", "/trash/"+p.ID+"/restore", map[string]int64{"revision": saved.Revision + 1}, &admin, nil)
	expect(200, code, "restore")
	saved.Revision += 2
	if e = app.RecoverUser(ctx, "member-"+nonadmin, t.TempDir()+"/denied"); e == nil {
		t.Fatal("member recovery must be denied")
	}
	// Imported summary status can be stale. Recalculation while adding a person
	// must not be mistaken for starting the blocked summary manually.
	code, b = request("POST", "/projects", payload, &admin, nil)
	expect(201, code, "regression project")
	var regression Project
	json.Unmarshal(b, &regression)
	regression.Tasks = []Task{
		{ID: "root", Title: "Summary", Status: "todo", Responsibility: "internal", Assignments: []Assignment{}},
		{ID: "pred", Title: "Predecessor", Status: "todo", Responsibility: "customer", Start: "2026-10-23", End: "2026-10-23", Assignments: []Assignment{}},
		{ID: "active", Parent: "root", Title: "Active child", Status: "doing", Responsibility: "customer", Start: "2026-10-23", End: "2026-10-23", Assignments: []Assignment{}},
		{ID: "wait", Parent: "root", Title: "Waiting child", Status: "todo", Responsibility: "internal", Planned: 600, DurationDays: 2, Assignments: []Assignment{}},
	}
	regression.Links = []Link{{"pred", "wait", "after", 0}}
	code, b = request("PUT", "/projects/"+regression.ID, saveRequest{Project: regression}, &admin, nil)
	expect(200, code, "summary rollup is not a manual start")
	json.Unmarshal(b, &regression)
	if _, e = store.DB.Exec(ctx, "UPDATE tasks SET status='todo' WHERE project_id=$1 AND id='root'", regression.ID); e != nil {
		t.Fatal(e)
	}
	regression.People = base.People
	code, b = request("PUT", "/projects/"+regression.ID, saveRequest{Project: regression}, &admin, nil)
	expect(200, code, "adding employee with stale blocked summary")
	json.Unmarshal(b, &regression)
	code, b = request("GET", "/projects/"+regression.ID, nil, &admin, nil)
	expect(200, code, "manual duration persisted")
	var reread struct {
		Project Project `json:"project"`
	}
	json.Unmarshal(b, &reread)
	for _, task := range reread.Project.Tasks {
		if task.ID == "wait" && task.DurationDays != 2 {
			t.Fatal("duration lost")
		}
	}
	for i := range regression.Tasks {
		if regression.Tasks[i].ID == "wait" {
			regression.Tasks[i].Status = "doing"
		}
	}
	code, _ = request("PUT", "/projects/"+regression.ID, saveRequest{Project: regression}, &admin, nil)
	expect(422, code, "real blocked leaf start still rejected")
	if e = store.Migrate(ctx); e != nil {
		t.Fatal("migration repeat", e)
	}
	t.Run("shared capacity and participation settings", func(t *testing.T) {
		emp := Employee{ID: "shared-capacity", Name: "Shared synthetic", Daily: 480}
		status, _ := request("POST", "/employees", emp, &admin, nil)
		expect(200, status, "capacity employee")
		decodeP := func(body []byte) Project {
			t.Helper()
			var v Project
			if err := json.Unmarshal(body, &v); err != nil {
				t.Fatal(err)
			}
			return v
		}
		get := func(id string) Project {
			t.Helper()
			code, body := request("GET", "/projects/"+id, nil, &admin, nil)
			expect(200, code, "capacity reload")
			var v struct{ Project Project }
			if err := json.Unmarshal(body, &v); err != nil {
				t.Fatal(err)
			}
			return v.Project
		}
		save := func(v Project) Project {
			t.Helper()
			code, body := request("PUT", "/projects/"+v.ID, saveRequest{Project: v}, &admin, nil)
			if code != 200 {
				t.Fatalf("capacity save: %d", code)
			}
			return decodeP(body)
		}
		create := func(title string, quota int) Project {
			code, body := request("POST", "/projects", map[string]string{"title": title, "start": "2026-10-23", "calendar_id": cal.ID, "duration_mode": "working"}, &admin, nil)
			expect(201, code, "capacity project")
			v := decodeP(body)
			week := map[string]Activity{}
			for _, day := range []string{"1", "2", "3", "4", "5"} {
				week[day] = Activity{Active: true, Start: "09:00", End: "18:00", Daily: quota}
			}
			v.People = []Participation{{EmployeeID: emp.ID, Week: week, Exceptions: map[string]Activity{}}}
			return save(v)
		}
		first := create("Visible capacity project", 360)
		second := create("HIDDEN_PROJECT_SENTINEL", 240)
		defer func() {
			for _, id := range []string{first.ID, second.ID} {
				v := get(id)
				code, _ := request("DELETE", "/projects/"+id, map[string]int64{"revision": v.Revision}, &admin, nil)
				expect(200, code, "capacity fixture cleanup")
			}
		}()
		first.Tasks = []Task{{ID: "capacity-task", Title: "Capacity task", Status: "todo", Responsibility: "internal", RequestedStart: first.Start, Planned: 360, Assignments: []Assignment{{EmployeeID: emp.ID, Daily: 360}}}}
		first = save(first)
		if first.CalculationState != "ready" || first.Tasks[0].Start != "2026-10-23" || len(first.Loads) != 1 {
			t.Fatal("idle project quota reserved capacity")
		}
		second.Tasks = []Task{{ID: "private-task-sentinel", Title: "HIDDEN_TASK_SENTINEL", Status: "todo", Responsibility: "internal", RequestedStart: second.Start, Planned: 240, Assignments: []Assignment{{EmployeeID: emp.ID, Daily: 240}}}}
		second = save(second)
		if second.CalculationState != "blocked_capacity" || second.Tasks[0].Start != "" || len(second.Loads) != 0 || second.Tasks[0].Planned != 240 {
			t.Fatal("conflicting configuration not saved as uncalculated")
		}
		originalRevision := first.Revision
		first = get(first.ID)
		if first.CalculationState != "blocked_capacity" || first.Revision != originalRevision || first.Tasks[0].End != "" {
			t.Fatal("GET did not refresh state after other project changed")
		}
		warning := strings.Join(first.Warnings, " ")
		for _, hidden := range []string{second.ID, second.Title, second.Tasks[0].ID, second.Tasks[0].Title} {
			if strings.Contains(warning, hidden) {
				t.Fatal("shared-capacity warning leaks hidden project data")
			}
		}

		oldRaw, err := readProject(ctx, store.DB, first.ID)
		if err != nil {
			t.Fatal(err)
		}
		memberView := copyStartProject(t, first)
		memberView.Tasks[0].Brief = "Authorized member note"
		if err = memberChange(Principal{User: User{EmployeeID: emp.ID}}, "member", oldRaw, memberView); err != nil {
			t.Fatal("computed capacity state/dates prevent member brief edit", err)
		}

		memberID := newID()
		sealed, _ := seal(cfg.Secret, []byte(totpSecret()))
		if _, err = store.DB.Exec(ctx, "INSERT INTO users(id,username,name,password_hash,totp,active) VALUES($1,$2,'Capacity member','unused',$3,true)", memberID, "capacity-member-"+memberID, sealed); err != nil {
			t.Fatal(err)
		}
		memberSession := testSession{token(), token()}
		if _, err = store.DB.Exec(ctx, "INSERT INTO sessions(hash,user_id,csrf) VALUES($1,$2,$3)", hash(memberSession.cookie), memberID, memberSession.csrf); err != nil {
			t.Fatal(err)
		}
		if _, err = store.DB.Exec(ctx, "INSERT INTO memberships(project_id,user_id,role) VALUES($1,$2,'member')", first.ID, memberID); err != nil {
			t.Fatal(err)
		}
		emp.UserID = memberID
		memberStatus, _ := request("POST", "/employees", emp, &admin, nil)
		expect(200, memberStatus, "bind capacity member")
		memberStatus, _ = request("GET", "/projects/"+second.ID, nil, &memberSession, nil)
		expect(404, memberStatus, "capacity other project hidden")
		memberStatus, memberBody := request("GET", "/projects/"+first.ID, nil, &memberSession, nil)
		expect(200, memberStatus, "capacity member read")
		var memberEnvelope struct{ Project Project }
		if err = json.Unmarshal(memberBody, &memberEnvelope); err != nil {
			t.Fatal(err)
		}
		for _, hidden := range []string{second.ID, second.Title, second.Tasks[0].ID, second.Tasks[0].Title} {
			if strings.Contains(string(memberBody), hidden) {
				t.Fatal("member response leaks hidden capacity project")
			}
		}
		memberEnvelope.Project.Tasks[0].Brief = "Allowed brief from blocked GET"
		memberStatus, memberBody = request("PUT", "/projects/"+first.ID, saveRequest{Project: memberEnvelope.Project}, &memberSession, nil)
		expect(200, memberStatus, "member brief from blocked GET")
		first = decodeP(memberBody)
		originalRevision = first.Revision
		if first.CalculationState != "blocked_capacity" || first.Tasks[0].Brief != "Allowed brief from blocked GET" {
			t.Fatal("member brief not saved while capacity blocked")
		}
		// Export must recompute derived state instead of returning stale stored dates.
		exportStatus, exportBody := request("GET", "/projects/"+first.ID+"/export", nil, &admin, nil)
		expect(200, exportStatus, "capacity export")
		exported := decodeP(exportBody)
		if exported.CalculationState != "blocked_capacity" || exported.Tasks[0].Start != "" {
			t.Fatal("export published stale calculated dates")
		}
		// Lower real competing work; the configured project quota remains four hours.
		second.Tasks[0].Planned = 120
		second = save(second)
		if second.CalculationState != "ready" {
			t.Fatal("lower real work did not resolve capacity")
		}
		first = get(first.ID)
		if first.CalculationState != "ready" || first.Tasks[0].End != "2026-10-23" || first.Revision != originalRevision {
			t.Fatal("resolved capacity did not restore schedule")
		}

		// A member's status change must not freeze forged computed dates as history.
		forged := copyStartProject(t, first)
		forged.Tasks[0].Status = "done"
		forged.Tasks[0].Start = "2099-01-01"
		forged.Tasks[0].End = "2099-01-02"
		for _, op := range []struct{ method, suffix string }{{"POST", "/preview"}, {"PUT", ""}} {
			status, body := request(op.method, "/projects/"+first.ID+op.suffix, saveRequest{Project: forged}, &memberSession, nil)
			expect(200, status, "member done derives authoritative dates")
			result := decodeP(body)
			if result.Tasks[0].Start != "2026-10-23" || result.Tasks[0].End != "2026-10-23" || result.Tasks[0].Status != "done" {
				t.Fatal("member froze forged historical dates")
			}
		}
		persisted, err := readProject(ctx, store.DB, first.ID)
		if err != nil {
			t.Fatal(err)
		}
		if persisted.Tasks[0].Start != "2026-10-23" || persisted.Tasks[0].End != "2026-10-23" {
			t.Fatal("forged dates reached storage")
		}
		first = get(first.ID)
		first.Tasks[0].Status = "todo"
		first = save(first)
		emp.Daily = 240
		code, _ := request("POST", "/employees", emp, &admin, nil)
		expect(200, code, "lower global limit")
		if get(first.ID).CalculationState != "blocked_capacity" {
			t.Fatal("employee limit change used stale cached loads")
		}
		emp.Daily = 480
		code, _ = request("POST", "/employees", emp, &admin, nil)
		expect(200, code, "restore global limit")
		if get(first.ID).CalculationState != "ready" {
			t.Fatal("global limit restoration did not restore ready state")
		}

		// Deliberately disable an existing binding; save as draft, then recover.
		configured := copyStartProject(t, second)
		second.People[0].Week = map[string]Activity{"1": {Active: false}}
		second = save(second)
		if second.CalculationState != "blocked_capacity" || second.Tasks[0].Start != "" || second.Tasks[0].Planned != 120 {
			t.Fatal("disabled existing binding did not preserve draft")
		}
		if get(first.ID).CalculationState != "blocked_capacity" {
			t.Fatal("disabled shared assignment was ignored")
		}
		second.People = configured.People
		second = save(second)
		if second.CalculationState != "ready" || second.Tasks[0].End != "2026-10-23" || second.Tasks[0].Planned != 120 {
			t.Fatal("reenabling participation did not recover calculation")
		}
		if get(first.ID).CalculationState != "ready" {
			t.Fatal("reenabling shared participant did not restore other project")
		}
		extra := Employee{ID: "new-participant", Name: "New participant", Daily: 480}
		code, _ = request("POST", "/employees", extra, &admin, nil)
		expect(200, code, "new participant")
		missing := copyStartProject(t, first)
		missing.People = append(missing.People, Participation{EmployeeID: extra.ID, Exceptions: map[string]Activity{}})
		for _, op := range []struct{ method, suffix string }{{"POST", "/preview"}, {"PUT", ""}} {
			code, _ = request(op.method, "/projects/"+first.ID+op.suffix, saveRequest{Project: missing}, &admin, nil)
			expect(422, code, "new binding requires settings")
		}

		inactive := copyStartProject(t, missing)
		inactive.People[len(inactive.People)-1].Week = map[string]Activity{"1": {Active: false}}
		code, _ = request("PUT", "/projects/"+first.ID, saveRequest{Project: inactive}, &admin, nil)
		expect(422, code, "new binding needs active participation")
		if len(get(first.ID).People) != 1 {
			t.Fatal("rejected binding persisted")
		}
		// Existing legacy bindings can be saved as draft; shared plans fail closed.
		if _, err := store.DB.Exec(ctx, "UPDATE participation SET week='{}' WHERE project_id=$1", second.ID); err != nil {
			t.Fatal(err)
		}
		second = get(second.ID)
		if second.CalculationState != "needs_participation" || len(second.Loads) != 0 {
			t.Fatal("legacy participation not marked")
		}
		second.Brief = "Legacy settings draft"
		second = save(second)
		if second.CalculationState != "needs_participation" {
			t.Fatal("legacy draft cannot be saved")
		}
		if get(first.ID).CalculationState != "blocked_capacity" {
			t.Fatal("shared missing participation did not fail closed")
		}
	})
	t.Run("migration 003 preserves legacy participation", func(t *testing.T) {
		name := "migration_" + newID()
		if _, err := root.DB.Exec(ctx, "CREATE SCHEMA "+name); err != nil {
			t.Fatal(err)
		}
		defer root.DB.Exec(ctx, "DROP SCHEMA "+name+" CASCADE")
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		q.Set("search_path", name)
		u.RawQuery = q.Encode()
		legacy, err := Open(ctx, u.String())
		if err != nil {
			t.Fatal(err)
		}
		defer legacy.DB.Close()
		for i, file := range []string{"schema.sql", "schema-002.sql"} {
			data, err := schema.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = legacy.DB.Exec(ctx, string(data)); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(data)
			if _, err = legacy.DB.Exec(ctx, "INSERT INTO schema_versions(version,checksum) VALUES($1,$2)", i+1, hex.EncodeToString(sum[:])); err != nil {
				t.Fatal(err)
			}
		}
		a := App{Store: legacy}
		if err = a.SeedCalendar(ctx, cal); err != nil {
			t.Fatal(err)
		}
		for _, sql := range []string{"INSERT INTO employees(id,name,daily_minutes) VALUES('legacy-person','Legacy',480)", "INSERT INTO projects(id,title,start_date,duration_mode,calendar_id) VALUES('legacy-plan','Legacy','2026-10-23','working','test')", "INSERT INTO participation(project_id,employee_id,weekdays,window_start,window_end) VALUES('legacy-plan','legacy-person','{1,3,5}','10:00','15:00')"} {
			if _, err = legacy.DB.Exec(ctx, sql); err != nil {
				t.Fatal(err)
			}
		}
		if err = legacy.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if err = legacy.Migrate(ctx); err != nil {
			t.Fatal("repeat migration", err)
		}
		v, err := readProject(ctx, legacy.DB, "legacy-plan")
		if err != nil {
			t.Fatal(err)
		}
		if len(v.People) != 1 || len(v.People[0].Week) != 0 || v.People[0].WindowStart != "10:00" || !reflect.DeepEqual(v.People[0].Weekdays, []int{1, 3, 5}) {
			t.Fatal("migration changed legacy settings or invented quotas")
		}
		if err = legacy.prepare(ctx, legacy.DB, &v); err != nil || v.CalculationState != "needs_participation" {
			t.Fatal("legacy migrated plan must require participation", err)
		}
	})

	runCommentIntegration(t, app, request, &admin)
	runProvisioningIntegration(t, app, request, &admin)

	code, b = request("GET", "/integrations", nil, &admin, nil)
	expect(200, code, "integration state")
	if !strings.Contains(string(b), `"enabled":false`) || strings.Contains(string(b), "password") {
		t.Fatal("unsafe integration response")
	}
	if e = app.workerTick(ctx); e != nil {
		t.Fatal(e)
	}
	var sending int
	if e = store.DB.QueryRow(ctx, "SELECT count(*) FROM outbox WHERE provider<>'smtp' AND state<>'paused'").Scan(&sending); e != nil || sending != 0 {
		t.Fatal("disabled worker created sendable work")
	}
	code, _ = request("POST", "/auth/logout", map[string]any{}, &admin, nil)
	expect(200, code, "logout")
	code, _ = request("GET", "/auth/me", nil, &admin, nil)
	expect(401, code, "session revoked")

	recoveryFile := t.TempDir() + "/recovery-url.txt"
	if e = app.RecoverUser(ctx, "admin", recoveryFile); e != nil {
		t.Fatal(e)
	}
	st, e := os.Stat(recoveryFile)
	if e != nil || st.Mode().Perm() != 0600 {
		t.Fatal("private recovery output required")
	}
	link, e := os.ReadFile(recoveryFile)
	if e != nil {
		t.Fatal(e)
	}
	recoveryToken := strings.TrimPrefix(string(link), cfg.Origin+"/#setup=")
	var active bool
	var credentials string
	var sessions, tokens int
	if e = store.DB.QueryRow(ctx, "SELECT active,password_hash FROM users WHERE username='admin'").Scan(&active, &credentials); e != nil || active || credentials != "" {
		t.Fatal("recovery must disable and clear old password", e)
	}
	store.DB.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE user_id=(SELECT id FROM users WHERE username='admin')").Scan(&sessions)
	store.DB.QueryRow(ctx, "SELECT count(*) FROM api_tokens WHERE user_id=(SELECT id FROM users WHERE username='admin')").Scan(&tokens)
	if sessions != 0 || tokens != 0 {
		t.Fatal("recovery failed to revoke access")
	}
	var seconds float64
	if e = store.DB.QueryRow(ctx, "SELECT extract(epoch from expires_at-now())::float8 FROM enrollments WHERE hash=$1", hash(recoveryToken)).Scan(&seconds); e != nil || seconds <= 0 || seconds > 900 {
		t.Fatal("recovery expiry", e)
	}
	if e = app.RecoverUser(ctx, "admin", recoveryFile); e == nil {
		t.Fatal("must not overwrite recovery output")
	}
	code, b = request("POST", "/auth/bootstrap", map[string]string{"token": recoveryToken, "username": "admin", "name": "Recovered", "password": "a different correct horse battery"}, nil, nil)
	expect(200, code, "local admin reenrollment")
	json.Unmarshal(b, &enroll)
	code, _ = request("POST", "/auth/bootstrap/confirm", map[string]string{"token": recoveryToken, "code": totpAt(enroll.Secret, time.Now().Unix()/30)}, nil, nil)
	expect(200, code, "local admin recovery confirmation")
	code, _ = request("POST", "/auth/bootstrap", map[string]string{"token": recoveryToken, "username": "admin", "name": "Again", "password": "a different correct horse battery"}, nil, nil)
	expect(403, code, "recovery token single use")
	if _, e = store.DB.Exec(ctx, "UPDATE users SET active=false WHERE admin"); e != nil {
		t.Fatal(e)
	}
	code, b = request("GET", "/auth/status", nil, nil, nil)
	expect(200, code, "status after deactivation")
	if !strings.Contains(string(b), `"setup_required":false`) {
		t.Fatal("setup reopened")
	}
	// Prove the permanent marker also survives deleting or demoting every admin.
	if _, e = store.DB.Exec(ctx, "UPDATE users SET admin=false WHERE admin"); e != nil {
		t.Fatal(e)
	}
	code, _ = request("POST", "/auth/bootstrap", map[string]string{"username": "replacement", "name": "Replacement", "password": "correct horse battery staple"}, nil, nil)
	expect(403, code, "tokenless setup cannot bypass consumption")
	t.Log("PostgreSQL migration, MFA/replay, CSRF, RBAC, concurrent revisions, cycles, free setup and integration pause verified")
	_ = fmt.Sprintf
}
