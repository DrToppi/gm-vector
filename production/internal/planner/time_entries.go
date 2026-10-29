package planner

import (
	"net/http"
)

func (a *App) timeEntries(w http.ResponseWriter, r *http.Request, u Principal) error {
	if _, e := a.requireProject(r, u, false); e != nil {
		return e
	}
	rows, e := a.Store.DB.Query(r.Context(), "SELECT id,task_id,employee_id,work_date::text,minutes,comment FROM time_entries WHERE project_id=$1 ORDER BY work_date DESC,id LIMIT 1000", r.PathValue("id"))
	if e != nil {
		return e
	}
	defer rows.Close()
	out := []TimeEntry{}
	for rows.Next() {
		var v TimeEntry
		if e = rows.Scan(&v.ID, &v.TaskID, &v.EmployeeID, &v.Date, &v.Minutes, &v.Comment); e != nil {
			return e
		}
		out = append(out, v)
	}
	if e = rows.Err(); e != nil {
		return e
	}
	jsonResponse(w, 200, out)
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
	if _, e = tx.Exec(ctx, "SELECT id FROM projects WHERE id=$1 FOR UPDATE", r.PathValue("id")); e != nil {
		return e
	}
	role, e = a.lockedRole(ctx, tx, u, r.PathValue("id"))
	if e != nil {
		return e
	}
	var employee string
	e = tx.QueryRow(ctx, "SELECT employee_id FROM time_entries WHERE id=$1 AND project_id=$2 FOR UPDATE", r.PathValue("entry"), r.PathValue("id")).Scan(&employee)
	if e != nil {
		return e
	}
	if role != "manager" && employee != u.EmployeeID {
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
