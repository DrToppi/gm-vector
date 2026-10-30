package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type multiYearRequest func(string, string, any, *testSession, *string) (int, json.RawMessage)

func testMultiYearAPI(t *testing.T, store *Store, request multiYearRequest, admin *testSession) {
	t.Helper()
	ctx := context.Background()
	call := func(want int, method, path string, body any, session *testSession) json.RawMessage {
		t.Helper()
		status, raw := request(method, path, body, session, nil)
		if status != want {
			t.Fatalf("%s %s expected%d got%d: %s", method, path, want, status, raw)
		}
		return raw
	}
	decode := func(raw json.RawMessage) Project {
		t.Helper()
		var v Project
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	calendars := map[int]Calendar{}
	for _, year := range []int{2025, 2026, 2027} {
		c := annualFixture(t, year)
		if year == 2027 {
			c.Days["2027-01-01"] = Day{}
			recalculateChecksum(t, &c)
		}
		calendars[year] = c
		call(201, "POST", "/calendars", c, admin)
	}
	raw := call(201, "POST", "/projects", map[string]any{"title": "Synthetic multi-year API", "start": "2026-01-01", "duration_mode": "calendar", "calendar_id": calendars[2026].ID}, admin)
	p := decode(raw)
	path := "/projects/" + p.ID
	p.Tasks = []Task{{ID: "long-task", Title: "731-day API duration", Status: "todo", Responsibility: "internal", RequestedStart: "2026-01-01", DurationDays: 731, Assignments: []Assignment{}}}
	raw = call(200, "POST", path+"/preview", saveRequest{Project: p}, admin)
	preview := decode(raw)
	if preview.Tasks[0].End != "2028-01-03" || !reflect.DeepEqual(preview.ProvisionalYears, []int{2027, 2028}) {
		t.Fatal("long preview", preview.Tasks[0], preview.ProvisionalYears)
	}
	raw = call(200, "PUT", path, saveRequest{Project: preview}, admin)
	p = decode(raw)
	p.Tasks[0].RequestedStart = "2026-12-31"
	p.Tasks[0].DurationDays = 2
	raw = call(200, "PUT", path, saveRequest{Project: p}, admin)
	p = decode(raw)
	if p.Tasks[0].End != "2027-01-01" {
		t.Fatal("provisional Jan1 not weekday", p.Tasks[0])
	}
	original := p
	p.CalendarVersions = map[string]string{"2027": calendars[2027].ID}
	raw = call(200, "POST", path+"/preview", saveRequest{Project: p}, admin)
	preview = decode(raw)
	if preview.Tasks[0].End != "2027-01-04" || len(preview.ProvisionalYears) != 0 {
		t.Fatal("verified preview did not replace provisional holiday", preview)
	}
	var view struct {
		Project Project `json:"project"`
	}
	json.Unmarshal(call(200, "GET", path, nil, admin), &view)
	if len(view.Project.CalendarVersions) != 0 || view.Project.Tasks[0].End != "2027-01-01" {
		t.Fatal("preview persisted calendar/date changes")
	}
	raw = call(200, "PUT", path, saveRequest{Project: p}, admin)
	p = decode(raw)
	call(409, "PUT", path, saveRequest{Project: original}, admin)
	raw = call(200, "POST", path+"/undo", map[string]int64{"revision": p.Revision}, admin)
	p = decode(raw)
	if len(p.CalendarVersions) != 0 || p.Tasks[0].End != "2027-01-01" {
		t.Fatal("undo did not restore empty pins", p.CalendarVersions, p.Tasks[0].End)
	}
	p.CalendarVersions = map[string]string{"2027": calendars[2027].ID}
	p = decode(call(200, "PUT", path, saveRequest{Project: p}, admin))
	p.CalendarVersions = map[string]string{}
	p = decode(call(200, "PUT", path, saveRequest{Project: p}, admin))
	p = decode(call(200, "POST", path+"/undo", map[string]int64{"revision": p.Revision}, admin))
	if p.CalendarVersions["2027"] != calendars[2027].ID {
		t.Fatal("undo lost previous pin")
	}
	// Old clients omit the new field. Their normal save must not erase selected years.
	transport, _ := json.Marshal(p)
	var legacy map[string]any
	json.Unmarshal(transport, &legacy)
	delete(legacy, "calendar_versions")
	raw = call(200, "PUT", path, map[string]any{"project": legacy}, admin)
	p = decode(raw)
	if p.CalendarVersions["2027"] != calendars[2027].ID {
		t.Fatal("legacy PUT cleared selected annual calendar")
	}
	memberID := newID()
	member := testSession{cookie: token(), csrf: token()}
	if _, err := store.DB.Exec(ctx, "INSERT INTO users(id,username,name,password_hash,totp,active) VALUES($1,$2,'Synthetic member','fixture',$3,true)", memberID, "calendar-member-"+memberID, []byte("synthetic-not-login")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.Exec(ctx, "INSERT INTO sessions(hash,user_id,csrf) VALUES($1,$2,$3)", hash(member.cookie), memberID, member.csrf); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.Exec(ctx, "INSERT INTO memberships VALUES($1,$2,'member')", p.ID, memberID); err != nil {
		t.Fatal(err)
	}
	// A legacy member can save without changing the anchor or supplying pins.
	transport, _ = json.Marshal(p)
	json.Unmarshal(transport, &legacy)
	delete(legacy, "calendar_versions")
	p = decode(call(200, "PUT", path, map[string]any{"project": legacy}, &member))
	if p.CalendarVersions["2027"] != calendars[2027].ID {
		t.Fatal("unchanged-anchor legacy member save lost an annual selection")
	}
	changed := p
	changed.CalendarVersions = map[string]string{}
	call(403, "POST", path+"/preview", saveRequest{Project: changed}, &member)
	call(403, "PUT", path, saveRequest{Project: changed}, &member)
	changed = p
	changed.CalendarVersions = map[string]string{"2028": calendars[2027].ID}
	call(422, "POST", path+"/preview", saveRequest{Project: changed}, admin)
	changed.CalendarVersions = map[string]string{"2026": calendars[2027].ID}
	call(422, "POST", path+"/preview", saveRequest{Project: changed}, admin)
	if _, err := store.DB.Exec(ctx, "UPDATE calendars SET days=days-'2027-01-02' WHERE id=$1", calendars[2027].ID); err != nil {
		t.Fatal(err)
	}
	call(422, "POST", path+"/preview", saveRequest{Project: p}, admin)
	json.Unmarshal(call(200, "GET", path, nil, admin), &view)
	if view.Project.CalculationState == "ready" || view.Project.Tasks[0].End != "" {
		t.Fatal("corrupt pinned year exposed computed dates", view.Project)
	}
	days, _ := json.Marshal(calendars[2027].Days)
	if _, err := store.DB.Exec(ctx, "UPDATE calendars SET days=$2 WHERE id=$1", calendars[2027].ID, days); err != nil {
		t.Fatal(err)
	}
	exported := call(200, "GET", path+"/export", nil, admin)
	var export map[string]json.RawMessage
	json.Unmarshal(exported, &export)
	for _, key := range []string{"loads", "warnings", "provisional_years"} {
		if _, ok := export[key]; ok {
			t.Fatal("export persisted derived field", key)
		}
	}
	if _, ok := export["calendar_versions"]; !ok {
		t.Fatal("export lost annual pins")
	}
	call(201, "POST", "/import", map[string]any{"project": json.RawMessage(exported)}, admin)
	var snapshots int
	if err := store.DB.QueryRow(ctx, "SELECT count(*) FROM snapshots WHERE project_id=$1 AND (payload ? 'loads' OR payload ? 'warnings' OR payload ? 'provisional_years' OR payload ? 'calculation_state')", p.ID).Scan(&snapshots); err != nil || snapshots != 0 {
		t.Fatal("snapshot derived payload", snapshots, err)
	}
	var emptyPinSnapshot int
	if err := store.DB.QueryRow(ctx, "SELECT count(*) FROM snapshots WHERE project_id=$1 AND payload->'calendar_versions'='{}'::jsonb", p.ID).Scan(&emptyPinSnapshot); err != nil || emptyPinSnapshot == 0 {
		t.Fatal("empty map snapshot omitted", emptyPinSnapshot, err)
	}
	testMultiYearLegacyAnchorChanges(t, call, admin, calendars[2027])
	testMultiYearBudgetGET(t, store, call, admin, calendars[2026].ID)
}

func testMultiYearLegacyAnchorChanges(t *testing.T, call func(int, string, string, any, *testSession) json.RawMessage, admin *testSession, c27 Calendar) {
	t.Helper()
	decode := func(raw json.RawMessage) Project {
		t.Helper()
		var p Project
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	legacyBody := func(p Project) any {
		t.Helper()
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		delete(fields, "calendar_versions")
		return map[string]any{"project": fields}
	}
	c26 := annualFixture(t, 2026)
	c26.ID = "synthetic-2026-legacy-anchor"
	c26.Days["2026-04-01"] = Day{}
	recalculateChecksum(t, &c26)
	call(201, "POST", "/calendars", c26, admin)
	p := decode(call(201, "POST", "/projects", map[string]any{"title": "Synthetic legacy anchor compatibility", "start": "2026-04-01", "duration_mode": "working", "calendar_id": c26.ID}, admin))
	path := "/projects/" + p.ID
	p.Tasks = []Task{{ID: "anchor-holiday", Title: "Verified holiday compatibility", Status: "todo", Responsibility: "internal", RequestedStart: "2026-04-01", DurationDays: 1, Assignments: []Assignment{}}}
	p = decode(call(200, "PUT", path, saveRequest{Project: p}, admin))
	if len(p.CalendarVersions) != 0 || p.Tasks[0].End != "2026-04-02" {
		t.Fatal("legacy anchor fixture did not retain its verified holiday", p.CalendarVersions, p.Tasks[0].End)
	}
	p.CalendarID = c27.ID
	preview := decode(call(200, "POST", path+"/preview", legacyBody(p), admin))
	if preview.CalendarVersions["2026"] != c26.ID || preview.Tasks[0].End != "2026-04-02" || len(preview.ProvisionalYears) != 0 {
		t.Fatal("legacy cross-year anchor preview lost the old verified year", preview.CalendarVersions, preview.Tasks[0].End, preview.ProvisionalYears)
	}
	p = decode(call(200, "PUT", path, legacyBody(p), admin))
	if p.CalendarVersions["2026"] != c26.ID || p.Tasks[0].End != "2026-04-02" || len(p.ProvisionalYears) != 0 {
		t.Fatal("legacy cross-year anchor save lost the old verified year", p.CalendarVersions, p.Tasks[0].End, p.ProvisionalYears)
	}
	// An explicit empty map remains an intentional clearing operation.
	p.CalendarVersions = map[string]string{}
	p = decode(call(200, "PUT", path, saveRequest{Project: p}, admin))
	if len(p.CalendarVersions) != 0 || p.Tasks[0].End != "2026-04-01" || !reflect.DeepEqual(p.ProvisionalYears, []int{2026}) {
		t.Fatal("explicit empty pins did not clear the former anchor year", p.CalendarVersions, p.Tasks[0].End, p.ProvisionalYears)
	}
	// Keep the existing same-year inherited-pin replacement contract.
	p.CalendarVersions = map[string]string{"2027": c27.ID}
	p.Tasks[0].RequestedStart = "2027-01-01"
	p = decode(call(200, "PUT", path, saveRequest{Project: p}, admin))
	if p.Tasks[0].End != "2027-01-04" {
		t.Fatal("same-year anchor fixture", p.Tasks[0].End)
	}
	replacement := annualFixture(t, 2027)
	replacement.ID = "synthetic-2027-legacy-replacement"
	replacement.Days["2027-01-01"], replacement.Days["2027-01-04"] = Day{}, Day{}
	recalculateChecksum(t, &replacement)
	call(201, "POST", "/calendars", replacement, admin)
	p.CalendarID = replacement.ID
	p = decode(call(200, "PUT", path, legacyBody(p), admin))
	if p.CalendarVersions["2027"] != replacement.ID || p.Tasks[0].End != "2027-01-05" || len(p.ProvisionalYears) != 0 {
		t.Fatal("same-year legacy anchor replacement lost inherited-pin normalization", p.CalendarVersions, p.Tasks[0].End)
	}
}

func testMultiYearBudgetGET(t *testing.T, store *Store, call func(int, string, string, any, *testSession) json.RawMessage, admin *testSession, calendarID string) {
	ctx := context.Background()
	id := newID()
	if _, err := store.DB.Exec(ctx, "INSERT INTO projects(id,title,start_date,duration_mode,calendar_id) VALUES($1,'Synthetic budget fallback','2025-01-01','working',$2)", id, calendarID); err != nil {
		t.Fatal(err)
	}
	queries := []string{
		"INSERT INTO employees(id,name,daily_minutes) SELECT $1||'-e-'||n,'Budget employee',480 FROM generate_series(1,300)n",
		"INSERT INTO tasks(project_id,id,title,status,responsibility,requested_start,start_date,end_date,planned_minutes) SELECT $1,$1||'-t-'||n,'Long task','todo','internal','2025-01-01','2025-01-01','2030-01-01',120000 FROM generate_series(1,300)n",
		`INSERT INTO participation(project_id,employee_id,weekdays,window_start,window_end,week) SELECT $1,$1||'-e-'||n,ARRAY[1,2,3,4,5],'09:00','18:00','{"1":{"active":true,"start":"09:00","end":"18:00","daily_minutes":480},"2":{"active":true,"start":"09:00","end":"18:00","daily_minutes":480},"3":{"active":true,"start":"09:00","end":"18:00","daily_minutes":480},"4":{"active":true,"start":"09:00","end":"18:00","daily_minutes":480},"5":{"active":true,"start":"09:00","end":"18:00","daily_minutes":480}}'::jsonb FROM generate_series(1,300)n`,
		"INSERT INTO assignments(project_id,task_id,employee_id,daily_minutes) SELECT $1,$1||'-t-'||n,$1||'-e-'||n,120 FROM generate_series(1,300)n",
	}
	for _, sql := range queries {
		if _, err := store.DB.Exec(ctx, sql, id); err != nil {
			t.Fatal(err)
		}
	}
	raw := call(200, "GET", "/projects/"+id, nil, admin)
	var out struct {
		Project Project `json:"project"`
	}
	json.Unmarshal(raw, &out)
	if out.Project.CalculationState != "blocked_capacity" || len(out.Project.Loads) != 0 || len(out.Project.Tasks) != 300 {
		t.Fatal("budget GET not editable", out.Project.CalculationState, len(out.Project.Loads))
	}
	for _, task := range out.Project.Tasks {
		if task.Start != "" || task.End != "" {
			t.Fatal("budget GET leaked partial dates")
		}
	}
	if !strings.Contains(strings.Join(out.Project.Warnings, " "), "budget") {
		t.Fatal("budget GET warning missing")
	}
	var retained int
	if err := store.DB.QueryRow(ctx, "SELECT count(*) FROM tasks WHERE project_id=$1 AND start_date='2025-01-01' AND end_date='2030-01-01'", id).Scan(&retained); err != nil || retained != 300 {
		t.Fatal("GET mutated stored history", retained, err)
	}
	raw = call(422, "POST", "/projects/"+id+"/preview", saveRequest{Project: out.Project}, admin)
	if !strings.Contains(string(raw), "calculation_budget_exceeded") {
		t.Fatal("wrong budget code", string(raw))
	}
	// Remove the intentionally blocked fixture so it cannot affect later tests.
	if _, err := store.DB.Exec(ctx, "DELETE FROM projects WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.Exec(ctx, "DELETE FROM employees WHERE id LIKE $1", id+"-e-%"); err != nil {
		t.Fatal(err)
	}
}

var _ = http.StatusOK
var _ = fmt.Sprintf
