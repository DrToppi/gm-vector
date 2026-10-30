package planner

import (
	"context"
	"encoding/json"
	"net/http"
)

type ImportRequest struct {
	Project Project `json:"project"`
	Preview bool    `json:"preview"`
}

func (a *App) exportProject(w http.ResponseWriter, r *http.Request, u Principal) error {
	role, e := a.requireProject(r, u, false)
	if e != nil {
		return e
	}
	if role != "manager" {
		return denied()
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
		return e
	}
	if e = audit(ctx, tx, u.ID, p.ID, "project.exported", requestID(r), nil); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	w.Header().Set("Content-Disposition", `attachment; filename="gma-project.json"`)
	exported := snapshotProject(p)
	exported.CalculationState = p.CalculationState // Keep the existing small readiness hint; omit the large ledger.
	jsonResponse(w, 200, exported)
	return nil
}
func (a *App) importProject(w http.ResponseWriter, r *http.Request, u Principal) error {
	if !u.Admin || u.Token {
		return denied()
	}
	var in ImportRequest
	if e := decode(r, &in); e != nil {
		return e
	}
	p := in.Project
	p.ID = newID()
	p.Revision = 0
	p.ExternalReference = ""
	p.Warnings = nil
	p.Loads = nil
	if _, e := validateProject(&p); e != nil {
		return bad(e.Error())
	}
	if len(p.People) > 0 {
		return bad("Для переноса назначений используйте миграцию с сопоставлением сотрудников; обычный импорт не создаёт людей")
	}
	for i := range p.Tasks {
		p.Tasks[i].Assignments = []Assignment{}
		p.Tasks[i].Actual = p.Tasks[i].LegacyActual
	}
	if p.CalendarVersions == nil {
		p.CalendarVersions = map[string]string{}
	}
	if _, e := readProjectCalendar(r.Context(), a.Store.DB, &p); e != nil {
		return calculationFailure(e)
	}
	// Ignore caller-supplied computed state and never echo stale imported dates.
	if e := func() error {
		tx, e := a.Store.DB.Begin(r.Context())
		if e != nil {
			return e
		}
		defer tx.Rollback(r.Context())
		if e = capacityLock(r.Context(), tx, true); e != nil {
			return e
		}
		return a.Store.prepare(r.Context(), tx, &p)
	}(); e != nil {
		return calculationFailure(e)
	}
	if in.Preview {
		jsonResponse(w, 200, map[string]any{"project": p, "message": "Будет создан новый проект без внешней привязки; доступы и внешняя отправка не импортируются"})
		return nil
	}
	if e := a.Import(r.Context(), p, u.ID, requestID(r)); e != nil {
		return e
	}
	jsonResponse(w, 201, p)
	return nil
}
func (a *App) Import(ctx context.Context, p Project, actor, req string) error {
	if _, e := validateProject(&p); e != nil {
		return bad(e.Error())
	}
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
	pins, e := json.Marshal(p.CalendarVersions)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO projects(id,title,start_date,duration_mode,calendar_id,external_reference,calendar_versions) VALUES($1,$2,$3,$4,$5,$6,$7)", p.ID, p.Title, p.Start, p.Mode, p.CalendarID, p.ExternalReference, pins); e != nil {
		return e
	}
	if e = a.Store.prepare(ctx, tx, &p); e != nil {
		return e
	}
	if e = writeProject(ctx, tx, p); e != nil {
		return e
	}
	if e = audit(ctx, tx, actor, p.ID, "project.imported", req, map[string]int{"tasks": len(p.Tasks)}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (a *App) SeedCalendar(ctx context.Context, c Calendar) error {
	if e := validateCalendar(&c); e != nil {
		return e
	}
	raw, _ := json.Marshal(c.Days)
	_, e := a.Store.DB.Exec(ctx, "INSERT INTO calendars VALUES($1,$2,$3,$4,$5,$6,now()) ON CONFLICT(id) DO NOTHING", c.ID, c.Year, c.Title, c.Source, c.Checksum, raw)
	return e
}
