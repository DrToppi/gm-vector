package planner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func historyCall(t *testing.T, f *readPathFixture, h http.Handler, method, path string, body any, user int) *httptest.ResponseRecorder {
	t.Helper()
	r := f.request(method, path, body, "")
	if user != 0 {
		r.Header.Del("Cookie")
		r.AddCookie(&http.Cookie{Name: "__Host-gma", Value: f.sessions[user].cookie})
		r.Header.Set("X-CSRF-Token", f.sessions[user].csrf)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func historyDecode[T any](t *testing.T, w *httptest.ResponseRecorder, want int) T {
	t.Helper()
	var v T
	if w.Code != want {
		t.Fatalf("expected HTTP%d got%d: %s", want, w.Code, w.Body.String())
	}
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil {
		t.Fatal(e)
	}
	return v
}
func seedHistoryRows(t *testing.T, f *readPathFixture, n int, comment string) {
	t.Helper()
	if n == 0 {
		return
	}
	_, e := f.store.DB.Exec(context.Background(), `INSERT INTO time_entries(id,project_id,task_id,employee_id,work_date,minutes,comment,actor) SELECT 'history-'||lpad(n::text,6,'0'),$1,'profile-task-000',CASE WHEN n=$2 THEN NULL ELSE 'profile-employee-00' END,'2024-02-29',1,$3,'profile-user-00' FROM generate_series(1,$2::integer)n`, f.projects[0].ID, n, comment)
	if e != nil {
		t.Fatal(e)
	}
}
func historyErrorCode(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	var out struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	out = historyDecode[struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}](t, w, status)
	if out.Error.Code != code {
		t.Fatalf("expected error%s got%s", code, out.Error.Code)
	}
}
