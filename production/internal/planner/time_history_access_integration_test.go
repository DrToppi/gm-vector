package planner

import (
	"context"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestTimeHistoryRevisionAccessIntegration(t *testing.T) {
	f := newReadPathFixture(t, 1)
	seedHistoryRows(t, f, 1001, "Synthetic guarded history")
	ctx := context.Background()
	h := f.app.Handler()
	id := f.projects[0].ID
	path := "/projects/" + id
	page := func(query string) HistoryPage {
		return historyDecode[HistoryPage](t, historyCall(t, f, h, "GET", path+"/time-history?limit=1"+query, nil, 0), 200)
	}
	first := page("")
	if first.NextCursor == nil {
		t.Fatal("missing initial cursor")
	}
	cursor := *first.NextCursor
	for _, query := range []string{"limit=0", "limit=501", "limit=wrong", "date_from=2024-02-30", "date_from=2025-01-01&date_to=2024-01-01", "employee_id=bad%20id", "limit=1&limit=2"} {
		historyErrorCode(t, historyCall(t, f, h, "GET", path+"/time-history?"+query, nil, 0), 422, "validation")
	}
	historyErrorCode(t, historyCall(t, f, h, "GET", path+"/time-history?task_id=not-in-this-project", nil, 0), 404, "not_found")
	for _, query := range []string{"&cursor=invalid", "&cursor=" + url.QueryEscape("A"+cursor[1:]), "&cursor=" + url.QueryEscape(cursor) + "&employee_id=other"} {
		historyErrorCode(t, historyCall(t, f, h, "GET", path+"/time-history?limit=1"+query, nil, 0), 422, "invalid_cursor")
	}
	historyErrorCode(t, historyCall(t, f, h, "GET", path+"/time-history?limit=100&cursor="+url.QueryEscape(cursor), nil, 0), 422, "invalid_cursor")
	principal := Principal{User: User{ID: "profile-user-00"}}
	expired, e := decodeHistoryCursor(cursor, f.app.Config.Secret, id, principal, HistoryFilters{}, 1, time.Now().UTC())
	if e != nil {
		t.Fatal(e)
	}
	expired.Expires = time.Now().Add(-time.Minute).Unix()
	expired.Issued = expired.Expires - int64(historyCursorTTL/time.Second)
	raw, _ := encodeHistoryCursor(expired, f.app.Config.Secret)
	historyErrorCode(t, historyCall(t, f, h, "GET", path+"/time-history?limit=1&cursor="+url.QueryEscape(raw), nil, 0), 422, "cursor_expired")
	if _, e = f.store.DB.Exec(ctx, "UPDATE memberships SET role='viewer' WHERE project_id=$1 AND user_id='profile-user-01'", id); e != nil {
		t.Fatal(e)
	}
	historyDecode[HistoryPage](t, historyCall(t, f, h, "GET", path+"/time-history", nil, 1), 200)
	historyErrorCode(t, historyCall(t, f, h, "GET", path+"/time-history?limit=1&cursor="+url.QueryEscape(cursor), nil, 1), 422, "invalid_cursor")
	historyErrorCode(t, historyCall(t, f, h, "GET", path+"/time-history/export", nil, 1), 403, "forbidden")
	anonymous := f.request("GET", path+"/time-history", nil, "")
	anonymous.Header.Del("Cookie")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, anonymous)
	historyErrorCode(t, w, 401, "unauthorized")
	service := token()
	if _, e = f.store.DB.Exec(ctx, `INSERT INTO api_tokens(hash,user_id,name,project_ids,scopes,expires_at) VALUES($1,'profile-user-00','Synthetic history token',ARRAY[$2],ARRAY['project:read'],now()+interval '1 hour')`, hash(service), id); e != nil {
		t.Fatal(e)
	}
	serviceCall := func(suffix string) *httptest.ResponseRecorder {
		r := f.request("GET", path+suffix, nil, "")
		r.Header.Del("Cookie")
		r.Header.Set("Authorization", "Bearer "+service)
		out := httptest.NewRecorder()
		h.ServeHTTP(out, r)
		return out
	}
	historyDecode[HistoryPage](t, serviceCall("/time-history"), 200)
	historyErrorCode(t, serviceCall("/time-history?limit=1&cursor="+url.QueryEscape(cursor)), 422, "invalid_cursor")
	historyErrorCode(t, serviceCall("/time-history/export"), 403, "forbidden")
	if _, e = f.store.DB.Exec(ctx, "DELETE FROM api_tokens WHERE hash=$1", hash(service)); e != nil {
		t.Fatal(e)
	}
	historyErrorCode(t, serviceCall("/time-history"), 401, "unauthorized")
	if _, e = f.store.DB.Exec(ctx, "DELETE FROM memberships WHERE project_id=$1 AND user_id='profile-user-01'", id); e != nil {
		t.Fatal(e)
	}
	historyErrorCode(t, historyCall(t, f, h, "GET", path+"/time-history?limit=1&cursor="+url.QueryEscape(cursor), nil, 1), 404, "not_found")
	if _, e = f.store.DB.Exec(ctx, "INSERT INTO memberships VALUES($1,'profile-user-01','member')", id); e != nil {
		t.Fatal(e)
	}
	// An unbound member does not own an unattributed NULL-employee record.
	historyErrorCode(t, historyCall(t, f, h, "DELETE", path+"/time-entries/history-001001", nil, 1), 403, "forbidden")
	before := page("")
	historyDecode[map[string]any](t, historyCall(t, f, h, "DELETE", path+"/time-entries/history-001001", nil, 0), 200)
	historyErrorCode(t, historyCall(t, f, h, "GET", path+"/time-history?limit=1&cursor="+url.QueryEscape(*before.NextCursor), nil, 0), 409, "history_changed")
	before = page("")
	historyDecode[TimeEntry](t, historyCall(t, f, h, "POST", path+"/time-entries", TimeEntry{TaskID: "profile-task-000", EmployeeID: "profile-employee-00", Date: "1999-12-30", Minutes: 15}, 0), 201)
	historyErrorCode(t, historyCall(t, f, h, "GET", path+"/time-history?limit=1&cursor="+url.QueryEscape(*before.NextCursor), nil, 0), 409, "history_changed")
	before = page("")
	view := historyDecode[struct {
		Project Project `json:"project"`
	}](t, historyCall(t, f, h, "GET", path, nil, 0), 200)
	p := view.Project
	p.Title = "Synthetic changed title"
	p = historyDecode[Project](t, historyCall(t, f, h, "PUT", path, saveRequest{Project: p}, 0), 200)
	historyErrorCode(t, historyCall(t, f, h, "GET", path+"/time-history?limit=1&cursor="+url.QueryEscape(*before.NextCursor), nil, 0), 409, "history_changed")
	before = page("&task_id=profile-task-000")
	p.Tasks = p.Tasks[1:]
	links := []Link{}
	for _, l := range p.Links {
		if l.From != "profile-task-000" && l.To != "profile-task-000" {
			links = append(links, l)
		}
	}
	p.Links = links
	p = historyDecode[Project](t, historyCall(t, f, h, "PUT", path, saveRequest{Project: p}, 0), 200)
	historyErrorCode(t, historyCall(t, f, h, "GET", path+"/time-history?limit=1&task_id=profile-task-000&cursor="+url.QueryEscape(*before.NextCursor), nil, 0), 409, "history_changed")
	if _, e = f.store.DB.Exec(ctx, "UPDATE users SET admin=true WHERE id='profile-user-09'"); e != nil {
		t.Fatal(e)
	}
	other := historyDecode[Project](t, historyCall(t, f, h, "POST", "/projects", map[string]any{"title": "Other synthetic project", "start": "2026-01-01", "duration_mode": "working", "calendar_id": f.projects[0].CalendarID}, 9), 201)
	historyErrorCode(t, historyCall(t, f, h, "GET", "/projects/"+other.ID+"/time-history?limit=1&cursor="+url.QueryEscape(cursor), nil, 9), 422, "invalid_cursor")
	historyDecode[map[string]any](t, historyCall(t, f, h, "DELETE", path, map[string]int64{"revision": p.Revision}, 0), 200)
	historyErrorCode(t, historyCall(t, f, h, "GET", path+"/time-history", nil, 0), 404, "not_found")
	for _, legacyPath := range []string{path + "/time-entries", "/projects/unknown-synthetic-project/time-entries"} {
		legacy := historyCall(t, f, h, "GET", legacyPath, nil, 9)
		historyDecode[[]TimeEntry](t, legacy, 200)
		if legacy.Header().Get("X-History-Total-Count") == "" {
			t.Fatal("legacy metadata missing")
		}
	}
}
