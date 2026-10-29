package planner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (a *App) employees(w http.ResponseWriter, r *http.Request, u Principal) error {
	all, e := readEmployees(r.Context(), a.Store.DB)
	if e != nil {
		return e
	}
	allowed := map[string]bool{}
	if !u.Admin {
		rows, err := a.Store.DB.Query(r.Context(), "SELECT DISTINCT p.employee_id FROM participation p JOIN memberships m ON m.project_id=p.project_id WHERE m.user_id=$1", u.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			allowed[id] = true
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
	}
	out := []Employee{}
	for _, v := range all {
		if u.Admin || allowed[v.ID] {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	jsonResponse(w, 200, out)
	return nil
}
func (a *App) saveEmployee(w http.ResponseWriter, r *http.Request, u Principal) error {
	if !u.Admin {
		return denied()
	}
	var v Employee
	if e := decode(r, &v); e != nil {
		return e
	}
	if v.ID == "" {
		v.ID = newID()
	}
	if !validID.MatchString(v.ID) || checkText(v.Name, 200, true) != nil || v.Daily < 0 || v.Daily > 1440 || len(v.ExternalID) > 256 || len(v.Provider) > 80 {
		return bad("Неверный сотрудник или дневной лимит")
	}
	ctx := r.Context()
	// Capacity edits use the stored source snapshot and work without a directory connection.
	before, e := readEmployees(ctx, a.Store.DB)
	if e != nil {
		return e
	}
	prior, existed := before[v.ID]
	sourceChanged := !existed || prior.Provider != v.Provider || prior.ExternalID != v.ExternalID
	if (v.Provider == "") != (v.ExternalID == "") || (v.Email != "" && !validEmail(v.Email)) {
		return bad("Invalid directory identity or email")
	}
	if v.Provider != "" {
		v.Email = ""
	}
	if !sourceChanged && v.Provider != "" {
		v.Email = prior.Email
	}
	if sourceChanged && v.Provider != "" && v.ExternalID != "" {
		person, e := a.resolveEmployee(ctx, v)
		if e != nil {
			return e
		}
		if person.ID != v.ExternalID || person.Provider != v.Provider {
			return bad("Источник вернул другого сотрудника")
		}
		if validEmail(person.Email) {
			v.Email = person.Email
		}
	}

	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(789502)"); e != nil {
		return e
	}
	if e = capacityLock(ctx, tx, false); e != nil {
		return e
	}
	current, e := readEmployees(ctx, tx)
	if e != nil {
		return e
	}
	now, present := current[v.ID]
	if existed != present || (present && (now.Provider != prior.Provider || now.ExternalID != prior.ExternalID || now.UserID != prior.UserID)) {
		return appError{409, "employee_changed", "Сотрудник изменён; обновите данные"}
	}
	if present && now.UserID != "" && sourceChanged {
		return bad("A linked account identity cannot be reassigned")
	}
	if present && now.UserID != "" && v.UserID == "" {
		return appError{409, "account_exists", "Аккаунт уже привязан; обновите карточку сотрудника"}
	}
	if present && !sourceChanged && v.Provider != "" {
		v.Email = now.Email
	}
	var changed bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM employees WHERE id=$1 AND (provider<>$2 OR external_id<>$3 OR coalesce(user_id,'')<>$4 OR email<>$5))", v.ID, v.Provider, v.ExternalID, v.UserID, v.Email).Scan(&changed); e != nil {
		return e
	}
	if changed {
		if e = revokeEmployeeInvites(ctx, tx, v.ID); e != nil {
			return e
		}
	}
	_, e = tx.Exec(ctx, "INSERT INTO employees(id,name,daily_minutes,user_id,provider,external_id,email) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(id) DO UPDATE SET name=excluded.name,daily_minutes=excluded.daily_minutes,user_id=excluded.user_id,provider=excluded.provider,external_id=excluded.external_id,email=excluded.email", v.ID, v.Name, v.Daily, nullable(v.UserID), v.Provider, v.ExternalID, v.Email)
	if e != nil {
		return bad("Не удалось сохранить сотрудника: проверьте привязку аккаунта")
	}
	if e = audit(ctx, tx, u.ID, "", "employee.changed", requestID(r), map[string]string{"employee_id": v.ID}); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	jsonResponse(w, 200, v)
	return nil
}
func (a *App) calendars(w http.ResponseWriter, r *http.Request, u Principal) error {
	rows, e := a.Store.DB.Query(r.Context(), "SELECT id,year,title,source,checksum,days FROM calendars ORDER BY year,id")
	if e != nil {
		return e
	}
	defer rows.Close()
	out := []Calendar{}
	for rows.Next() {
		var c Calendar
		var raw []byte
		if e = rows.Scan(&c.ID, &c.Year, &c.Title, &c.Source, &c.Checksum, &raw); e != nil {
			return e
		}
		if e = json.Unmarshal(raw, &c.Days); e != nil {
			return e
		}
		out = append(out, c)
	}
	if e = rows.Err(); e != nil {
		return e
	}
	jsonResponse(w, 200, out)
	return nil
}
func validateCalendar(c *Calendar) error {
	if !validID.MatchString(c.ID) || c.Year < 2000 || c.Year > 2200 || checkText(c.Title, 200, true) != nil || checkText(c.Source, 2000, true) != nil {
		return bad("Неверный календарь или нет источника")
	}
	start := time.Date(c.Year, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(1, 0, 0)
	n := 0
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		v, ok := c.Days[fmtDate(d)]
		if !ok {
			return bad("Нужны все даты года")
		}
		if v.Shortened && !v.Working {
			return bad("Сокращённый день должен быть рабочим")
		}
		n++
	}
	if len(c.Days) != n {
		return bad("Лишние даты календаря")
	}
	b, _ := json.Marshal(c.Days)
	sum := sha256.Sum256(b)
	digest := hex.EncodeToString(sum[:])
	if c.Checksum != "" && c.Checksum != digest {
		return bad("Контрольная сумма не совпадает")
	}
	c.Checksum = digest
	return nil
}
func (a *App) importCalendar(w http.ResponseWriter, r *http.Request, u Principal) error {
	if !u.Admin {
		return denied()
	}
	var c Calendar
	if e := decode(r, &c); e != nil {
		return e
	}
	if e := validateCalendar(&c); e != nil {
		return e
	}
	ctx := r.Context()
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	raw, _ := json.Marshal(c.Days)
	_, e = tx.Exec(ctx, "INSERT INTO calendars(id,year,title,source,checksum,days) VALUES($1,$2,$3,$4,$5,$6)", c.ID, c.Year, c.Title, c.Source, c.Checksum, raw)
	if e != nil {
		return bad("ID версии уже существует; создайте новую версию")
	}
	if e = audit(ctx, tx, u.ID, "", "calendar.imported", requestID(r), map[string]string{"calendar_id": c.ID, "checksum": c.Checksum}); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	jsonResponse(w, 201, c)
	return nil
}
func (a *App) integrations(w http.ResponseWriter, r *http.Request, u Principal) error {
	if !u.Admin {
		return denied()
	}
	jsonResponse(w, 200, map[string]any{"calendar": map[string]any{"enabled": false, "state": "not_configured"}, "smtp": map[string]any{"enabled": a.Config.SMTP.Enabled}, "directory": map[string]any{"ldap_configured": a.Config.LDAP != nil}})
	return nil
}
func (a *App) diagnostics(w http.ResponseWriter, r *http.Request, u Principal) error {
	if !u.Admin {
		return denied()
	}
	var jobs int
	if e := a.Store.DB.QueryRow(r.Context(), "SELECT count(*) FROM outbox WHERE state='paused'").Scan(&jobs); e != nil {
		return e
	}
	jsonResponse(w, 200, map[string]any{"version": Version, "database": "ready", "paused_events": jobs, "teams_enabled": false, "timezone": "Europe/Moscow", "backup": "См. локальный журнал резервирования"})
	return nil
}
func (a *App) auditLog(w http.ResponseWriter, r *http.Request, u Principal) error {
	if !u.Admin {
		return denied()
	}
	before := int64(9223372036854775807)
	if s := r.URL.Query().Get("before"); s != "" {
		n, e := strconv.ParseInt(s, 10, 64)
		if e != nil {
			return bad("Неверный курсор")
		}
		before = n
	}
	rows, e := a.Store.DB.Query(r.Context(), "SELECT id,coalesce(actor,''),coalesce(project_id,''),action,details,created_at FROM audit WHERE id<$1 ORDER BY id DESC LIMIT 100", before)
	if e != nil {
		return e
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var actor, pid, action string
		var raw json.RawMessage
		var at time.Time
		if e = rows.Scan(&id, &actor, &pid, &action, &raw, &at); e != nil {
			return e
		}
		out = append(out, map[string]any{"id": id, "actor": actor, "project_id": pid, "action": action, "details": raw, "created_at": at})
	}
	if e = rows.Err(); e != nil {
		return e
	}
	jsonResponse(w, 200, out)
	return nil
}
func (a *App) tokens(w http.ResponseWriter, r *http.Request, u Principal) error {
	if !u.Admin {
		return denied()
	}
	rows, e := a.Store.DB.Query(r.Context(), "SELECT hash,name,project_ids,scopes,expires_at FROM api_tokens WHERE user_id=$1", u.ID)
	if e != nil {
		return e
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name string
		var projects, scopes []string
		var until time.Time
		if e = rows.Scan(&id, &name, &projects, &scopes, &until); e != nil {
			return e
		}
		out = append(out, map[string]any{"id": id, "name": name, "project_ids": projects, "scopes": scopes, "expires_at": until})
	}
	if e = rows.Err(); e != nil {
		return e
	}
	jsonResponse(w, 200, out)
	return nil
}
func (a *App) createToken(w http.ResponseWriter, r *http.Request, u Principal) error {
	if !u.Admin {
		return denied()
	}
	var in struct {
		Name     string   `json:"name"`
		Projects []string `json:"project_ids"`
		Scopes   []string `json:"scopes"`
		Days     int      `json:"days"`
	}
	if e := decode(r, &in); e != nil {
		return e
	}
	if checkText(in.Name, 200, true) != nil || in.Days < 1 || in.Days > 90 || len(in.Projects) > 100 || len(in.Scopes) == 0 {
		return bad("Укажите имя, проекты, scopes и срок 1–90 дней")
	}
	if in.Projects == nil {
		in.Projects = []string{}
	}
	for _, s := range in.Scopes {
		if contains([]string{"project:read", "project:write", "export"}, s) && len(in.Projects) == 0 {
			return bad("Project scopes require explicit projects")
		}
		if !contains([]string{"project:read", "project:write", "export", "directory:read", "directory:write"}, s) {
			return bad("Неизвестный scope")
		}
	}
	ctx := r.Context()
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	for _, id := range in.Projects {
		var exists bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1 AND archived_at IS NULL)", id).Scan(&exists); e != nil {
			return e
		}
		if !exists {
			return bad("Неизвестный проект")
		}
	}
	raw := token()
	until := time.Now().Add(time.Duration(in.Days) * 24 * time.Hour)
	if _, e = tx.Exec(ctx, "INSERT INTO api_tokens(hash,user_id,name,project_ids,scopes,expires_at) VALUES($1,$2,$3,$4,$5,$6)", hash(raw), u.ID, in.Name, in.Projects, in.Scopes, until); e != nil {
		return e
	}
	if e = audit(ctx, tx, u.ID, "", "token.created", requestID(r), map[string]string{"name": in.Name}); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	jsonResponse(w, 201, map[string]any{"token": raw, "expires_at": until})
	return nil
}
func (a *App) revokeToken(w http.ResponseWriter, r *http.Request, u Principal) error {
	if !u.Admin {
		return denied()
	}
	tag, e := a.Store.DB.Exec(r.Context(), "DELETE FROM api_tokens WHERE hash=$1 AND user_id=$2", r.PathValue("id"), u.ID)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return denied()
	}
	if e = audit(r.Context(), a.Store.DB, u.ID, "", "token.revoked", requestID(r), nil); e != nil {
		return e
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
	return nil
}

var _ = strings.TrimSpace
