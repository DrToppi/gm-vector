package planner

import (
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/url"
	"strconv"
)

func (a *App) timeEntries(w http.ResponseWriter, r *http.Request, u Principal) error {
	if _, e := a.requireProject(r, u, false); e != nil {
		return e
	}
	ctx := r.Context()
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = capacityLock(ctx, tx, true); e != nil {
		return e
	}
	var allowed bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users u WHERE u.id=$2 AND u.active AND(u.admin OR EXISTS(SELECT 1 FROM memberships m WHERE m.project_id=$1 AND m.user_id=u.id)))`, r.PathValue("id"), u.ID).Scan(&allowed); e != nil {
		return e
	}
	if !allowed {
		return notFoundHistory()
	}
	var revision, total int64
	e = tx.QueryRow(ctx, "SELECT revision FROM projects WHERE id=$1", r.PathValue("id")).Scan(&revision)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM time_entries WHERE project_id=$1", r.PathValue("id")).Scan(&total); e != nil {
		return e
	}
	rows, e := tx.Query(ctx, "SELECT id,task_id,coalesce(employee_id,''),work_date::text,minutes,comment FROM time_entries WHERE project_id=$1 ORDER BY work_date DESC,id LIMIT 1000", r.PathValue("id"))
	if e != nil {
		return e
	}
	out := []TimeEntry{}
	for rows.Next() {
		var v TimeEntry
		if e = rows.Scan(&v.ID, &v.TaskID, &v.EmployeeID, &v.Date, &v.Minutes, &v.Comment); e != nil {
			rows.Close()
			return e
		}
		out = append(out, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	raw, e := materializeProjectRead(ctx, tx, out)
	if e != nil {
		return e
	}
	w.Header().Set("X-History-Total-Count", strconv.FormatInt(total, 10))
	w.Header().Set("X-History-Returned-Count", strconv.Itoa(len(out)))
	w.Header().Set("X-History-Truncated", strconv.FormatBool(total > int64(len(out))))
	w.Header().Set("X-History-Revision", strconv.FormatInt(revision, 10))
	w.Header().Set("Link", "</api/v1/projects/"+url.PathEscape(r.PathValue("id"))+"/time-history>; rel=\"alternate\"")
	writeMaterializedJSON(w, raw)
	return nil
}
func (a *App) addTimeEntry(w http.ResponseWriter, r *http.Request, u Principal) error {
	role, e := a.requireProject(r, u, true)
	if e != nil {
		return e
	}
	var v TimeEntry
	if e = decode(r, &v); e != nil {
		return e
	}
	if _, e = date(v.Date); e != nil {
		return bad("Неверная дата")
	}
	if v.Minutes < 1 || v.Minutes > 1440 || checkText(v.Comment, 4000, false) != nil {
		return bad("Трудозатраты: 1–1440 минут")
	}
	if role != "manager" && (u.EmployeeID == "" || v.EmployeeID != u.EmployeeID) {
		return denied()
	}
	ctx := r.Context()
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = capacityLock(ctx, tx, false); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "SELECT id FROM projects WHERE id=$1 FOR UPDATE", r.PathValue("id")); e != nil {
		return e
	}
	role, e = a.lockedRole(ctx, tx, u, r.PathValue("id"))
	if e != nil {
		return e
	}
	if role != "manager" && (u.EmployeeID == "" || v.EmployeeID != u.EmployeeID) {
		return denied()
	}
	var exists bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM assignments WHERE project_id=$1 AND task_id=$2 AND employee_id=$3)", r.PathValue("id"), v.TaskID, v.EmployeeID).Scan(&exists); e != nil {
		return e
	}
	if !exists {
		return bad("Сотрудник не назначен на задачу")
	}
	v.ID = newID()
	if _, e = tx.Exec(ctx, "INSERT INTO time_entries VALUES($1,$2,$3,$4,$5,$6,$7,$8)", v.ID, r.PathValue("id"), v.TaskID, v.EmployeeID, v.Date, v.Minutes, v.Comment, u.ID); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE projects SET revision=revision+1 WHERE id=$1", r.PathValue("id")); e != nil {
		return e
	}
	if e = audit(ctx, tx, u.ID, r.PathValue("id"), "time.added", requestID(r), map[string]any{"entry_id": v.ID, "minutes": v.Minutes}); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	jsonResponse(w, 201, v)
	return nil
}
func (a *App) deleteTimeEntry(w http.ResponseWriter, r *http.Request, u Principal) error {
	role, e := a.requireProject(r, u, true)
	if e != nil {
		return e
	}
	ctx := r.Context()
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = capacityLock(ctx, tx, false); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "SELECT id FROM projects WHERE id=$1 FOR UPDATE", r.PathValue("id")); e != nil {
		return e
	}
	role, e = a.lockedRole(ctx, tx, u, r.PathValue("id"))
	if e != nil {
		return e
	}
	var employee string
	e = tx.QueryRow(ctx, "SELECT coalesce(employee_id,'') FROM time_entries WHERE id=$1 AND project_id=$2 FOR UPDATE", r.PathValue("entry"), r.PathValue("id")).Scan(&employee)
	if e != nil {
		return e
	}
	if role != "manager" && (employee == "" || u.EmployeeID == "" || employee != u.EmployeeID) {
		return denied()
	}
	if _, e = tx.Exec(ctx, "DELETE FROM time_entries WHERE id=$1", r.PathValue("entry")); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE projects SET revision=revision+1 WHERE id=$1", r.PathValue("id")); e != nil {
		return e
	}
	if e = audit(ctx, tx, u.ID, r.PathValue("id"), "time.deleted", requestID(r), map[string]string{"entry_id": r.PathValue("entry")}); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
	return nil
}
