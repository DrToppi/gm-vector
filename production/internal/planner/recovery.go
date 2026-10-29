package planner

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"os"
	"strings"
)

// Called only after project FOR UPDATE, shared with membership changes.
func (a *App) lockedRole(ctx context.Context, tx pgx.Tx, u Principal, project string) (string, error) {
	var admin, active bool
	if e := tx.QueryRow(ctx, "SELECT admin,active FROM users WHERE id=$1", u.ID).Scan(&admin, &active); e != nil || !active {
		return "", denied()
	}
	if u.Token && !contains(u.Projects, project) {
		return "", denied()
	}
	if admin {
		return "manager", nil
	}
	var role string
	if e := tx.QueryRow(ctx, "SELECT role FROM memberships WHERE project_id=$1 AND user_id=$2", project, u.ID).Scan(&role); e != nil || role == "viewer" {
		return "", denied()
	}
	return role, nil
}
func (a *App) memberCandidates(w http.ResponseWriter, r *http.Request, u Principal) error {
	role, e := a.requireProject(r, u, false)
	if e != nil {
		return e
	}
	if role != "manager" || u.Token {
		return denied()
	}
	rows, e := a.Store.DB.Query(r.Context(), "SELECT id,name FROM users WHERE active ORDER BY name")
	if e != nil {
		return e
	}
	defer rows.Close()
	out := []map[string]string{}
	for rows.Next() {
		var id, name string
		if e = rows.Scan(&id, &name); e != nil {
			return e
		}
		out = append(out, map[string]string{"id": id, "name": name})
	}
	if e = rows.Err(); e != nil {
		return e
	}
	jsonResponse(w, 200, out)
	return nil
}
func (a *App) trash(w http.ResponseWriter, r *http.Request, u Principal) error {
	if !u.Admin || u.Token {
		return denied()
	}
	rows, e := a.Store.DB.Query(r.Context(), "SELECT id,title,revision,archived_at::text FROM projects WHERE archived_at IS NOT NULL ORDER BY archived_at DESC LIMIT 100")
	if e != nil {
		return e
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, title, at string
		var rev int64
		if e = rows.Scan(&id, &title, &rev, &at); e != nil {
			return e
		}
		out = append(out, map[string]any{"id": id, "title": title, "revision": rev, "archived_at": at})
	}
	if e = rows.Err(); e != nil {
		return e
	}
	jsonResponse(w, 200, out)
	return nil
}
func (a *App) restoreProject(w http.ResponseWriter, r *http.Request, u Principal) error {
	if !u.Admin || u.Token {
		return denied()
	}
	var in struct {
		Revision int64 `json:"revision"`
	}
	if e := decode(r, &in); e != nil {
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
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(789503)"); e != nil {
		return e
	}
	tag, e := tx.Exec(ctx, "UPDATE projects SET archived_at=NULL,revision=revision+1 WHERE id=$1 AND revision=$2 AND archived_at>now()-interval '30 days'", r.PathValue("id"), in.Revision)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return conflict()
	}
	if e = audit(ctx, tx, u.ID, r.PathValue("id"), "project.restored", requestID(r), nil); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
	return nil
}
func (a *App) RecoverUser(ctx context.Context, username, file string) error {
	if file == "" {
		return errors.New("an exclusive output file is required")
	}
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var uid string
	if e = tx.QueryRow(ctx, "SELECT id FROM users WHERE username=$1 AND admin AND auth_provider='local' FOR UPDATE", strings.ToLower(strings.TrimSpace(username))).Scan(&uid); e != nil {
		return e
	}
	for _, sql := range []string{"DELETE FROM sessions WHERE user_id=$1", "DELETE FROM api_tokens WHERE user_id=$1", "DELETE FROM enrollments WHERE user_id=$1", "UPDATE users SET active=false,password_hash='',totp='\\x',last_totp=-1,recovery_hashes='{}' WHERE id=$1"} {
		if _, e = tx.Exec(ctx, sql, uid); e != nil {
			return e
		}
	}
	raw := token()
	if _, e = tx.Exec(ctx, "INSERT INTO enrollments(hash,user_id,expires_at) VALUES($1,$2,now()+interval '15 minutes')", hash(raw), uid); e != nil {
		return e
	}
	if e = audit(ctx, tx, "", "", "auth.local_recovery", "local-cli", map[string]string{"user_id": uid}); e != nil {
		return e
	}
	if e = writeNew(file, []byte(a.Config.Origin+"/#setup="+raw)); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		_ = os.Remove(file)
		return e
	}
	return nil
}
