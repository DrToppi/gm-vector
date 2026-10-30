package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/http"
	"reflect"
	"strings"
)

func (a *App) projects(w http.ResponseWriter, r *http.Request, u Principal) error {
	rows, e := a.Store.DB.Query(r.Context(), `SELECT p.id,p.title,p.brief,p.revision,count(t.id) FROM projects p LEFT JOIN tasks t ON t.project_id=p.id WHERE p.archived_at IS NULL AND ($1 OR EXISTS(SELECT 1 FROM memberships m WHERE m.project_id=p.id AND m.user_id=$2)) GROUP BY p.id ORDER BY p.title`, u.Admin, u.ID)
	if e != nil {
		return e
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, title, brief string
		var rev int64
		var n int
		if e = rows.Scan(&id, &title, &brief, &rev, &n); e != nil {
			return e
		}
		if u.Token && !contains(u.Projects, id) {
			continue
		}
		out = append(out, map[string]any{"id": id, "title": title, "brief": brief, "revision": rev, "task_count": n})
	}
	if e = rows.Err(); e != nil {
		return e
	}
	jsonResponse(w, 200, out)
	return nil
}
func (a *App) getProject(w http.ResponseWriter, r *http.Request, u Principal) error {
	role, e := a.requireProject(r, u, false)
	if e != nil {
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
	p, e := readProject(ctx, tx, r.PathValue("id"))
	if e != nil {
		return e
	}
	if e = a.Store.prepare(ctx, tx, &p); e != nil {
		if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
			return e
		}
		var typed appError
		if errors.As(e, &typed) && typed.Code == "calculation_budget_exceeded" {
			// GET remains usable for legacy large metadata. Derived schedule is
			// withheld; preview/save must return the explicit budget error.
			invalidateSchedule(&p, "blocked_capacity", typed.Message)
		} else if !contains(p.Warnings, e.Error()) {
			p.Warnings = append(p.Warnings, e.Error())
		}
	}
	raw, e := materializeProjectRead(ctx, tx, map[string]any{"project": p, "role": role})
	if e != nil {
		return e
	}
	w.Header().Set("ETag", fmt.Sprintf(`"%d"`, p.Revision))
	writeMaterializedJSON(w, raw)
	return nil
}

// Complete encoding while the protected inputs are stable, then release the
// transaction before writing to a possibly slow or disconnected HTTP client.
func materializeProjectRead(ctx context.Context, tx pgx.Tx, value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if err = tx.Rollback(ctx); err != nil {
		return nil, err
	}
	return raw, nil
}
func writeMaterializedJSON(w http.ResponseWriter, raw []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(raw, '\n'))
}
func (a *App) createProject(w http.ResponseWriter, r *http.Request, u Principal) error {
	if !u.Admin || u.Token {
		return denied()
	}
	var in struct {
		Title            string            `json:"title"`
		Start            string            `json:"start"`
		CalendarID       string            `json:"calendar_id"`
		CalendarVersions map[string]string `json:"calendar_versions"`
		Mode             string            `json:"duration_mode"`
	}
	if e := decode(r, &in); e != nil {
		return e
	}
	p := Project{ID: newID(), Title: strings.TrimSpace(in.Title), Start: in.Start, Mode: in.Mode, CalendarID: in.CalendarID, CalendarVersions: in.CalendarVersions, Tasks: []Task{}, Links: []Link{}, People: []Participation{}}
	if _, e := validateProject(&p); e != nil {
		return bad(e.Error())
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
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(789503)"); e != nil {
		return e
	}
	if p.CalendarVersions == nil {
		p.CalendarVersions = map[string]string{}
	}
	if _, e = readProjectCalendar(ctx, tx, &p); e != nil {
		return calculationFailure(e)
	}
	pins, e := json.Marshal(p.CalendarVersions)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "INSERT INTO projects(id,title,start_date,duration_mode,calendar_id,calendar_versions) VALUES($1,$2,$3,$4,$5,$6)", p.ID, p.Title, p.Start, p.Mode, p.CalendarID, pins)
	if e != nil {
		return e
	}
	if e = audit(ctx, tx, u.ID, p.ID, "project.created", requestID(r), nil); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	jsonResponse(w, 201, p)
	return nil
}

type saveRequest struct {
	Project        Project `json:"project"`
	OverrideReason string  `json:"override_reason"`
}

func (a *App) previewProject(w http.ResponseWriter, r *http.Request, u Principal) error {
	role, e := a.requireProject(r, u, true)
	if e != nil {
		return e
	}
	var in saveRequest
	if e = decode(r, &in); e != nil {
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
	old, e := readProject(ctx, tx, r.PathValue("id"))
	if e != nil {
		return e
	}
	if in.Project.ID != old.ID || in.Project.Revision != old.Revision {
		return conflict()
	}
	if role != "manager" && in.Project.CalendarID != old.CalendarID {
		return denied()
	}
	if e = inheritCalendarVersions(ctx, tx, old, &in.Project); e != nil {
		return e
	}
	if e = memberChange(u, role, old, in.Project); e != nil {
		return e
	}
	if e = a.Store.normalizeMemberDates(ctx, tx, role, old, &in.Project); e != nil {
		return e
	}
	if e = validateNewParticipation(old, in.Project); e != nil {
		return e
	}
	if e = shiftProjectStart(old, &in.Project); e != nil {
		return bad(e.Error())
	}
	if e = a.Store.prepare(ctx, tx, &in.Project); e != nil {
		return calculationFailure(e)
	}
	raw, e := materializeProjectRead(ctx, tx, in.Project)
	if e != nil {
		return e
	}
	writeMaterializedJSON(w, raw)
	return nil
}
func memberChange(u Principal, role string, old, p Project) error {
	if role == "manager" {
		return nil
	}
	if role != "member" {
		return denied()
	}
	before, after := old, p
	before.CalculationState = ""
	after.CalculationState = ""
	before.Warnings = nil
	after.Warnings = nil
	before.Loads = nil
	after.Loads = nil
	before.ProvisionalYears = nil
	after.ProvisionalYears = nil
	before.Tasks = nil
	after.Tasks = nil
	if !reflect.DeepEqual(before, after) || len(old.Tasks) != len(p.Tasks) {
		return denied()
	}
	for i, t := range p.Tasks {
		b := old.Tasks[i]
		owned := false
		for _, ass := range b.Assignments {
			owned = owned || ass.EmployeeID == u.EmployeeID && u.EmployeeID != ""
		}
		if owned {
			t.Status = b.Status
			t.Brief = b.Brief
		}
		// Internal active dates are derived and may change after another project or global limit changes.
		// RequestedStart and DurationDays stay protected; completed/customer ranges stay immutable to members.
		if b.Status != "done" && b.Responsibility != "customer" {
			t.Start = b.Start
			t.End = b.End
		}
		t.Actual = b.Actual
		t.Summary = b.Summary
		t.Blocked = b.Blocked
		if !reflect.DeepEqual(t, b) {
			return denied()
		}
	}
	return nil
}

func (a *App) saveProject(w http.ResponseWriter, r *http.Request, u Principal) error {
	if _, e := a.requireProject(r, u, true); e != nil {
		return e
	}
	var in saveRequest
	if e := decode(r, &in); e != nil {
		return e
	}
	return a.commitProject(w, r, u, in, false)
}
func (a *App) commitProject(w http.ResponseWriter, r *http.Request, u Principal, in saveRequest, undo bool) error {
	ctx := r.Context()
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = capacityLock(ctx, tx, false); e != nil {
		return e
	}
	var rev int64
	id := r.PathValue("id")
	e = tx.QueryRow(ctx, "SELECT revision FROM projects WHERE id=$1 AND archived_at IS NULL FOR UPDATE", id).Scan(&rev)
	if e != nil {
		return e
	}
	role, e := a.lockedRole(ctx, tx, u, id)
	if e != nil {
		return e
	}
	old, e := readProject(ctx, tx, id)
	if e != nil {
		return e
	}
	p := in.Project
	if p.ID != id || p.Revision != rev {
		return conflict()
	}
	if undo && p.CalendarVersions == nil {
		p.CalendarVersions = map[string]string{}
	}
	if role != "manager" && p.CalendarID != old.CalendarID {
		return denied()
	}
	if e = inheritCalendarVersions(ctx, tx, old, &p); e != nil {
		return e
	}
	if e = memberChange(u, role, old, p); e != nil {
		return e
	}
	if e = a.Store.normalizeMemberDates(ctx, tx, role, old, &p); e != nil {
		return e
	}
	if !undo {
		if e = validateNewParticipation(old, p); e != nil {
			return e
		}
	}
	if p.ExternalReference != old.ExternalReference && role != "manager" {
		return denied()
	}
	if checkText(p.ExternalReference, 500, false) != nil {
		return bad("External reference is too long")
	}
	actual := map[string]int{}
	oldStatus := map[string]string{}
	legacyActual := map[string]int{}
	for _, t := range old.Tasks {
		actual[t.ID] = t.Actual
		legacyActual[t.ID] = t.LegacyActual
		oldStatus[t.ID] = t.Status
	}
	for i := range p.Tasks {
		p.Tasks[i].Actual = actual[p.Tasks[i].ID]
		p.Tasks[i].LegacyActual = legacyActual[p.Tasks[i].ID]
	}
	if !undo {
		if e = shiftProjectStart(old, &p); e != nil {
			return bad(e.Error())
		}
	}
	if e = a.Store.prepare(ctx, tx, &p); e != nil {
		return calculationFailure(e)
	}
	for _, t := range p.Tasks {
		if !t.Summary && t.Blocked && t.Status != "todo" && t.Status != oldStatus[t.ID] {
			if role != "manager" || len(strings.TrimSpace(in.OverrideReason)) < 5 {
				return bad(fmt.Sprintf("«%s»: старт заблокирован зависимостью. Руководитель может указать причину исключения", t.Title))
			}
		}
	}
	raw, _ := json.Marshal(snapshotProject(old))
	if _, e = tx.Exec(ctx, "INSERT INTO snapshots(project_id,revision,payload) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", id, rev, raw); e != nil {
		return e
	}
	p.Revision++
	if e = writeProject(ctx, tx, p); e != nil {
		return e
	}
	if e = cancelInvalidCustomerMail(ctx, tx, id); e != nil {
		return e
	}
	action := "project.changed"
	if undo {
		action = "project.undo"
	}
	if e = audit(ctx, tx, u.ID, id, action, requestID(r), map[string]any{"revision": p.Revision, "override_reason": in.OverrideReason}); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	jsonResponse(w, 200, p)
	return nil
}
func (a *App) undoProject(w http.ResponseWriter, r *http.Request, u Principal) error {
	role, e := a.requireProject(r, u, true)
	if e != nil {
		return e
	}
	if role != "manager" {
		return denied()
	}
	var in struct {
		Revision int64 `json:"revision"`
	}
	if e = decode(r, &in); e != nil {
		return e
	}
	var raw []byte
	e = a.Store.DB.QueryRow(r.Context(), "SELECT payload FROM snapshots WHERE project_id=$1 AND revision<$2 ORDER BY revision DESC LIMIT 1", r.PathValue("id"), in.Revision).Scan(&raw)
	if e != nil {
		return e
	}
	var p Project
	if e = json.Unmarshal(raw, &p); e != nil {
		return e
	}
	p.Revision = in.Revision
	return a.commitProject(w, r, u, saveRequest{Project: p}, true)
}
func (a *App) archiveProject(w http.ResponseWriter, r *http.Request, u Principal) error {
	role, e := a.requireProject(r, u, true)
	if e != nil {
		return e
	}
	if role != "manager" {
		return denied()
	}
	var in struct {
		Revision int64 `json:"revision"`
	}
	if e = decode(r, &in); e != nil {
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
	if role != "manager" {
		return denied()
	}
	tag, e := tx.Exec(ctx, "UPDATE projects SET archived_at=now(),revision=revision+1 WHERE id=$1 AND revision=$2 AND archived_at IS NULL", r.PathValue("id"), in.Revision)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return conflict()
	}
	if e = cancelInvalidCustomerMail(ctx, tx, r.PathValue("id")); e != nil {
		return e
	}
	if e = audit(ctx, tx, u.ID, r.PathValue("id"), "project.archived", requestID(r), nil); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
	return nil
}
func (a *App) members(w http.ResponseWriter, r *http.Request, u Principal) error {
	role, e := a.requireProject(r, u, false)
	if e != nil {
		return e
	}
	if role != "manager" {
		return denied()
	}
	rows, e := a.Store.DB.Query(r.Context(), "SELECT m.user_id,u.name,m.role FROM memberships m JOIN users u ON u.id=m.user_id WHERE project_id=$1 ORDER BY u.name", r.PathValue("id"))
	if e != nil {
		return e
	}
	defer rows.Close()
	out := []map[string]string{}
	for rows.Next() {
		var id, name, role string
		if e = rows.Scan(&id, &name, &role); e != nil {
			return e
		}
		out = append(out, map[string]string{"user_id": id, "name": name, "role": role})
	}
	if e = rows.Err(); e != nil {
		return e
	}
	jsonResponse(w, 200, out)
	return nil
}
func (a *App) setMember(w http.ResponseWriter, r *http.Request, u Principal) error {
	role, e := a.requireProject(r, u, true)
	if e != nil {
		return e
	}
	if role != "manager" {
		return denied()
	}
	var in struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if e = decode(r, &in); e != nil {
		return e
	}
	if in.Role != "" && !contains([]string{"manager", "member", "viewer"}, in.Role) {
		return bad("Неизвестная роль")
	}
	if in.UserID == u.ID && !u.Admin {
		return bad("Свою роль меняет администратор")
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
	if role != "manager" {
		return denied()
	}
	if in.Role == "" {
		_, e = tx.Exec(ctx, "DELETE FROM memberships WHERE project_id=$1 AND user_id=$2", r.PathValue("id"), in.UserID)
	} else {
		_, e = tx.Exec(ctx, "INSERT INTO memberships VALUES($1,$2,$3) ON CONFLICT(project_id,user_id) DO UPDATE SET role=excluded.role", r.PathValue("id"), in.UserID, in.Role)
	}
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "DELETE FROM sessions WHERE user_id=$1", in.UserID); e != nil {
		return e
	}
	if e = audit(ctx, tx, u.ID, r.PathValue("id"), "member.changed", requestID(r), in); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
	return nil
}

var _ = pgx.ErrNoRows

func validateNewParticipation(old, p Project) error {
	previous := map[string]bool{}
	for _, v := range old.People {
		previous[v.EmployeeID] = true
	}
	for _, v := range p.People {
		if previous[v.EmployeeID] {
			continue
		}
		active := false
		for _, day := range v.Week {
			active = active || day.Active
		}
		for _, day := range v.Exceptions {
			active = active || day.Active
		}
		if len(v.Week) == 0 || !active {
			return bad("Перед добавлением сотрудника настройте активные дни, окна и квоты участия")
		}
	}
	return nil
}

// A member may submit derived dates from a GET whose resource state changed
// without its own revision. Ignore that transport copy only after authorization,
// and replace it with a fresh server calculation BEFORE a status change to done
// can make the scheduler preserve the caller's date range as historical data.
func (s *Store) normalizeMemberDates(ctx context.Context, q DBTX, role string, old Project, p *Project) error {
	if role != "member" {
		return nil
	}
	raw, err := json.Marshal(old)
	if err != nil {
		return err
	}
	var authoritative Project
	if err = json.Unmarshal(raw, &authoritative); err != nil {
		return err
	}
	if err = s.prepare(ctx, q, &authoritative); err != nil {
		return err
	}
	dates := map[string]Task{}
	for _, t := range authoritative.Tasks {
		dates[t.ID] = t
	}
	for i, t := range old.Tasks {
		if t.Status != "done" && t.Responsibility != "customer" {
			p.Tasks[i].Start = dates[t.ID].Start
			p.Tasks[i].End = dates[t.ID].End
		}
	}
	return nil
}
