package planner

import (
	"context"
	"net/http"
)

type DirectoryPerson struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Provider string `json:"provider"`
}
type DirectoryEntry struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Email    string   `json:"email"`
	Username string   `json:"username"`
	Active   *bool    `json:"active"`
	Groups   []string `json:"groups"`
}
type directoryBatch struct {
	Provider string           `json:"provider"`
	Entries  []DirectoryEntry `json:"entries"`
}

func (a *App) resolveEmployee(ctx context.Context, v Employee) (DirectoryPerson, error) {
	if a.directoryLookup != nil {
		return a.directoryLookup(ctx, v)
	}
	if v.Provider == "" && v.ExternalID == "" {
		return DirectoryPerson{ID: "", Name: v.Name, Email: v.Email, Provider: ""}, nil
	}
	var p DirectoryPerson
	e := a.Store.DB.QueryRow(ctx, "SELECT external_id,name,email,provider FROM directory_entries WHERE provider=$1 AND external_id=$2 AND active", v.Provider, v.ExternalID).Scan(&p.ID, &p.Name, &p.Email, &p.Provider)
	if e != nil {
		return p, bad("Active directory identity not found")
	}
	return p, nil
}
func directoryAllowed(u Principal, scope string) bool {
	return u.Admin && (!u.Token || contains(u.Scopes, scope))
}
func (a *App) directoryPeople(w http.ResponseWriter, r *http.Request, u Principal) error {
	if !directoryAllowed(u, "directory:read") {
		return denied()
	}
	q := r.URL.Query().Get("q")
	if len(q) > 100 {
		return bad("Search is too long")
	}
	source, provider := r.URL.Query().Get("source"), r.URL.Query().Get("provider")
	if source != "" && source != "ldap" && source != "api" {
		return bad("Unknown directory source")
	}
	ldapProvider := ""
	if a.Config.LDAP != nil {
		ldapProvider = a.Config.LDAP.Provider
	}
	rows, e := a.Store.DB.Query(r.Context(), "SELECT external_id,name,email,provider FROM directory_entries WHERE active AND (strpos(lower(name),lower($1))>0 OR strpos(lower(email),lower($1))>0) AND ($2='' OR provider=$2) AND ($3='' OR ($3='ldap' AND provider=$4) OR ($3='api' AND provider<>$4)) ORDER BY name,provider,external_id LIMIT 100", q, provider, source, ldapProvider)
	if e != nil {
		return e
	}
	defer rows.Close()
	out := []DirectoryPerson{}
	for rows.Next() {
		var p DirectoryPerson
		if e = rows.Scan(&p.ID, &p.Name, &p.Email, &p.Provider); e != nil {
			return e
		}
		out = append(out, p)
	}
	if e = rows.Err(); e != nil {
		return e
	}
	jsonResponse(w, 200, out)
	return nil
}
func validateDirectoryBatch(b directoryBatch) error {
	if !validID.MatchString(b.Provider) || len(b.Entries) > 1000 {
		return bad("Invalid provider or batch size")
	}
	seen := map[string]bool{}
	for _, v := range b.Entries {
		if checkText(v.ID, 256, true) != nil || checkText(v.Name, 200, true) != nil || len(v.Username) > 200 || v.Active == nil || (v.Email != "" && !validEmail(v.Email)) || len(v.Groups) > 100 || seen[v.ID] {
			return bad("Invalid or duplicate directory entry")
		}
		seen[v.ID] = true
		for _, g := range v.Groups {
			if checkText(g, 200, true) != nil {
				return bad("Invalid group")
			}
		}
	}
	return nil
}
func (a *App) syncDirectory(w http.ResponseWriter, r *http.Request, u Principal) error {
	if !directoryAllowed(u, "directory:write") {
		return denied()
	}
	var b directoryBatch
	if e := decode(r, &b); e != nil {
		return e
	}
	if a.Config.LDAP != nil && b.Provider == a.Config.LDAP.Provider {
		return bad("LDAP provider is managed by the LDAP connector")
	}
	result, e := a.applyDirectory(r.Context(), b, u.ID, requestID(r))
	if e != nil {
		return e
	}
	jsonResponse(w, 200, result)
	return nil
}
func (a *App) applyDirectory(ctx context.Context, b directoryBatch, actor, rid string) (map[string]int, error) {
	if e := validateDirectoryBatch(b); e != nil {
		return nil, e
	}
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(789502)"); e != nil {
		return nil, e
	}
	ids := []string{}
	disabled := 0
	for _, v := range b.Entries {
		ids = append(ids, v.ID)
		if v.Groups == nil {
			v.Groups = []string{}
		}
		var did string
		e = tx.QueryRow(ctx, `INSERT INTO directory_entries(id,provider,external_id,name,email,username,active,groups) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(provider,external_id) DO UPDATE SET name=excluded.name,email=excluded.email,username=excluded.username,active=excluded.active,groups=excluded.groups,updated_at=now() RETURNING id`, newID(), b.Provider, v.ID, v.Name, v.Email, v.Username, *v.Active, v.Groups).Scan(&did)
		if e != nil {
			return nil, e
		}
		rows, err := tx.Query(ctx, "SELECT id,coalesce(user_id,''),email FROM employees WHERE provider=$1 AND external_id=$2", b.Provider, v.ID)
		if err != nil {
			return nil, err
		}
		type linked struct{ id, uid, email string }
		links := []linked{}
		for rows.Next() {
			var x linked
			if err = rows.Scan(&x.id, &x.uid, &x.email); err != nil {
				rows.Close()
				return nil, err
			}
			links = append(links, x)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if len(links) == 0 {
			_, e = tx.Exec(ctx, "INSERT INTO employees(id,name,daily_minutes,provider,external_id,email) VALUES($1,$2,480,$3,$4,$5)", newID(), v.Name, b.Provider, v.ID, v.Email)
			if e != nil {
				return nil, e
			}
		}
		for _, x := range links {
			if x.email != v.Email || !*v.Active {
				if e = revokeEmployeeInvites(ctx, tx, x.id); e != nil {
					return nil, e
				}
			}
			if !*v.Active && x.uid != "" {
				var admin bool
				if e = tx.QueryRow(ctx, "SELECT admin FROM users WHERE id=$1 FOR UPDATE", x.uid).Scan(&admin); e != nil {
					return nil, e
				}
				if admin {
					return nil, bad("Directory cannot deactivate a local administrator")
				}
				for _, sql := range []string{"UPDATE users SET active=false WHERE id=$1", "DELETE FROM sessions WHERE user_id=$1", "DELETE FROM api_tokens WHERE user_id=$1", "DELETE FROM enrollments WHERE user_id=$1"} {
					if _, e = tx.Exec(ctx, sql, x.uid); e != nil {
						return nil, e
					}
				}
				disabled++
			}
		}
		if _, e = tx.Exec(ctx, "UPDATE employees SET name=$3,email=$4 WHERE provider=$1 AND external_id=$2", b.Provider, v.ID, v.Name, v.Email); e != nil {
			return nil, e
		}
	}
	var missing int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM directory_entries WHERE provider=$1 AND NOT(external_id=ANY($2::text[]))", b.Provider, ids).Scan(&missing); e != nil {
		return nil, e
	}
	result := map[string]int{"processed": len(ids), "disabled": disabled, "missing": missing}
	if e = audit(ctx, tx, actor, "", "directory.synced", rid, map[string]any{"provider": b.Provider, "counts": result}); e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return result, nil
}
func (a *App) directoryStatus(w http.ResponseWriter, r *http.Request, u Principal) error {
	if !u.Admin || u.Token {
		return denied()
	}
	rows, e := a.Store.DB.Query(r.Context(), "SELECT DISTINCT provider FROM directory_entries ORDER BY provider")
	if e != nil {
		return e
	}
	defer rows.Close()
	sources := []string{}
	for rows.Next() {
		var s string
		if e = rows.Scan(&s); e != nil {
			return e
		}
		sources = append(sources, s)
	}
	if e = rows.Err(); e != nil {
		return e
	}
	enabled := a.Config.LDAP != nil && a.Config.LDAP.Enabled
	provider := ""
	if a.Config.LDAP != nil {
		provider = a.Config.LDAP.Provider
	}
	details := []map[string]string{}
	for _, s := range sources {
		kind := "api"
		if a.Config.LDAP != nil && s == a.Config.LDAP.Provider {
			kind = "ldap"
		}
		details = append(details, map[string]string{"provider": s, "kind": kind})
	}
	jsonResponse(w, 200, map[string]any{"source_details": details, "ldap": map[string]any{"configured": a.Config.LDAP != nil, "enabled": enabled, "provider": provider}, "sources": sources})
	return nil
}
func (a *App) syncLDAPDirectory(w http.ResponseWriter, r *http.Request, u Principal) error {
	if !u.Admin || u.Token {
		return denied()
	}
	if a.Config.LDAP == nil || !a.Config.LDAP.Enabled {
		return bad("LDAP is disabled")
	}
	entries, e := a.Config.LDAP.readEntries(r.Context(), "")
	if e != nil {
		return appError{503, "directory_unavailable", "LDAP synchronization failed; no data changed"}
	}
	out, e := a.applyDirectory(r.Context(), directoryBatch{Provider: a.Config.LDAP.Provider, Entries: entries}, u.ID, requestID(r))
	if e != nil {
		return e
	}
	jsonResponse(w, 200, out)
	return nil
}
