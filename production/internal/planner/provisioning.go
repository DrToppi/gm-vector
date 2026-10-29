package planner

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"
	"time"
)

type InviteStatus struct {
	ID            string    `json:"id"`
	EmployeeID    string    `json:"employee_id"`
	UserID        string    `json:"user_id"`
	Username      string    `json:"username"`
	Email         string    `json:"email"`
	State         string    `json:"state"`
	ExpiresAt     time.Time `json:"expires_at"`
	DeliveryState string    `json:"delivery_state"`
	DeliveryError string    `json:"delivery_error"`
}

func readInvite(ctx context.Context, q DBTX, id string) (InviteStatus, error) {
	var v InviteStatus
	e := q.QueryRow(ctx, `SELECT i.id::text,i.employee_id,i.user_id,i.username,i.email,least(i.expires_at,coalesce(en.expires_at,i.expires_at)),CASE WHEN u.active THEN 'active' WHEN i.revoked THEN 'revoked' WHEN i.expires_at<=now() OR en.hash IS NULL OR en.expires_at<=now() THEN 'expired' ELSE 'pending' END,coalesce(o.state,'failed'),coalesce(o.last_error,'') FROM employee_invitations i JOIN users u ON u.id=i.user_id LEFT JOIN enrollments en ON en.hash=i.token_hash LEFT JOIN outbox o ON o.id=i.id::text AND o.provider='smtp_invitation' WHERE i.id=$1`, id).Scan(&v.ID, &v.EmployeeID, &v.UserID, &v.Username, &v.Email, &v.ExpiresAt, &v.State, &v.DeliveryState, &v.DeliveryError)
	if v.DeliveryState == "sending" {
		v.DeliveryState = "queued"
	}
	return v, e
}
func (a *App) employeeInvite(w http.ResponseWriter, r *http.Request, u Principal) error {
	if !u.Admin {
		return denied()
	}
	ctx := r.Context()
	id := r.PathValue("id")
	var active bool
	if e := a.Store.DB.QueryRow(ctx, "SELECT coalesce(u.active,false) FROM employees e LEFT JOIN users u ON u.id=e.user_id WHERE e.id=$1", id).Scan(&active); e != nil {
		return e
	}
	var latest string
	var result any
	e := a.Store.DB.QueryRow(ctx, "SELECT id::text FROM employee_invitations WHERE employee_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1", id).Scan(&latest)
	if e == nil {
		v, e := readInvite(ctx, a.Store.DB, latest)
		if e != nil {
			return e
		}
		result = v
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	jsonResponse(w, 200, map[string]any{"invitation": result, "smtp_enabled": a.Config.SMTP.Enabled, "account_active": active})
	return nil
}
func (a *App) inviteEmployee(w http.ResponseWriter, r *http.Request, u Principal) error {
	if !u.Admin {
		return denied()
	}
	var in struct {
		ID      string `json:"id"`
		Reissue bool   `json:"reissue"`
	}
	if e := decode(r, &in); e != nil {
		return e
	}
	if !commentUUID.MatchString(in.ID) {
		return bad("Нужен UUID операции")
	}
	in.ID = strings.ToLower(in.ID)
	ctx := r.Context()
	id := r.PathValue("id")
	// Idempotent replay must not need an upstream lookup or resend an invitation.
	if prev, e := readInvite(ctx, a.Store.DB, in.ID); e == nil {
		var actor string
		var reissue bool
		if e = a.Store.DB.QueryRow(ctx, "SELECT actor,reissue FROM employee_invitations WHERE id=$1", in.ID).Scan(&actor, &reissue); e != nil {
			return e
		}
		if prev.EmployeeID != id || actor != u.ID || reissue != in.Reissue {
			return appError{409, "idempotency_conflict", "UUID уже использован другой операцией"}
		}
		jsonResponse(w, 200, prev)
		return nil
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	if !a.Config.SMTP.Enabled {
		return appError{503, "email_disabled", "SMTP не настроен; аккаунт не создан"}
	}
	all, e := readEmployees(ctx, a.Store.DB)
	if e != nil {
		return e
	}
	v, ok := all[id]
	if !ok {
		return pgx.ErrNoRows
	}
	person, e := a.resolveEmployee(ctx, v)
	if e != nil {
		return e
	}
	if person.ID != v.ExternalID || person.Provider != v.Provider || !validEmail(person.Email) {
		return bad("В источнике нет корректного email сотрудника")
	}
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	// Serialize invitation creation and source changes. Employee lock follows it.
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(789502)"); e != nil {
		return e
	}
	var current Employee
	if e = tx.QueryRow(ctx, "SELECT id,name,coalesce(user_id,''),provider,external_id,email FROM employees WHERE id=$1 FOR UPDATE", id).Scan(&current.ID, &current.Name, &current.UserID, &current.Provider, &current.ExternalID, &current.Email); e != nil {
		return e
	}

	// Concurrent same UUID returns the same operation after acquiring the lock.
	if prev, e := readInvite(ctx, tx, in.ID); e == nil {
		var actor string
		var reissue bool
		if e = tx.QueryRow(ctx, "SELECT actor,reissue FROM employee_invitations WHERE id=$1", in.ID).Scan(&actor, &reissue); e != nil {
			return e
		}
		if prev.EmployeeID != id || actor != u.ID || reissue != in.Reissue {
			return appError{409, "idempotency_conflict", "UUID уже использован"}
		}
		jsonResponse(w, 200, prev)
		return nil
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	if current.Provider != v.Provider || current.ExternalID != v.ExternalID || current.UserID != v.UserID || (a.directoryLookup == nil && current.Email != person.Email) {
		return appError{409, "employee_changed", "Сотрудник изменён; повторите операцию"}
	}
	var duplicate bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM employees WHERE id<>$1 AND provider=$2 AND external_id=$3 AND provider<>'' AND user_id IS NOT NULL)", id, v.Provider, v.ExternalID).Scan(&duplicate); e != nil {
		return e
	}
	if duplicate {
		return appError{409, "identity_exists", "Для этой записи источника уже создан аккаунт"}
	}
	authProvider, directoryID, directoryUsername := "local", "", ""
	if a.Config.LDAP != nil && current.Provider == a.Config.LDAP.Provider {
		if e = tx.QueryRow(ctx, "SELECT id,username FROM directory_entries WHERE provider=$1 AND external_id=$2 AND active", current.Provider, current.ExternalID).Scan(&directoryID, &directoryUsername); e != nil {
			return e
		}
		authProvider = "ldap"
	}
	uid := current.UserID
	username := ""
	if uid != "" {
		var active bool
		var ph string
		var enrolled bool
		if e = tx.QueryRow(ctx, "SELECT username,active,password_hash,octet_length(totp)>0 FROM users WHERE id=$1 FOR UPDATE", uid).Scan(&username, &active, &ph, &enrolled); e != nil {
			return e
		}
		if active || ph != "" || enrolled {
			return appError{409, "account_exists", "У сотрудника уже есть настроенный аккаунт; пароль не изменён"}
		}
		if !in.Reissue {
			return appError{409, "invitation_exists", "Приглашение уже создано; используйте явную повторную выдачу"}
		}
	} else if in.Reissue {
		return bad("Нет приглашения для повторной выдачи")
	}
	if uid == "" {
		uid = newID()
		username = "employee-" + uid
		if authProvider == "ldap" {
			username = directoryUsername
		}
		if _, e = tx.Exec(ctx, "INSERT INTO users(id,username,name,password_hash,totp,admin,auth_provider,directory_id) VALUES($1,$2,$3,'',$4,false,$5,$6)", uid, username, current.Name, []byte{}, authProvider, directoryID); e != nil {
			return e
		}
	}
	if e = revokeEmployeeInvites(ctx, tx, id); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "DELETE FROM enrollments WHERE user_id=$1", uid); e != nil {
		return e
	}
	raw := token()
	sealed, e := seal(a.Config.Secret, []byte(raw))
	if e != nil {
		return e
	}
	payload, _ := json.Marshal(map[string]any{"secret": sealed})
	if _, e = tx.Exec(ctx, "UPDATE employees SET user_id=$2,email=$3 WHERE id=$1", id, uid, person.Email); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO enrollments(hash,user_id,expires_at) VALUES($1,$2,now()+interval '24 hours')", hash(raw), uid); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO employee_invitations(id,employee_id,user_id,actor,username,email,provider,external_id,token_hash,reissue,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,now()+interval '24 hours')`, in.ID, id, uid, u.ID, username, person.Email, v.Provider, v.ExternalID, hash(raw), in.Reissue); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO outbox(id,provider,payload,state) VALUES($1,'smtp_invitation',$2,'queued')", in.ID, payload); e != nil {
		return e
	}
	if e = audit(ctx, tx, u.ID, "", "employee.invited", requestID(r), map[string]string{"employee_id": id, "invitation_id": in.ID}); e != nil {
		return e
	}
	result, e := readInvite(ctx, tx, in.ID)
	if e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	jsonResponse(w, 201, result)
	return nil
}
func revokeEmployeeInvites(ctx context.Context, q DBTX, id string) error {
	if _, e := q.Exec(ctx, "DELETE FROM enrollments WHERE hash IN (SELECT token_hash FROM employee_invitations WHERE employee_id=$1 AND NOT revoked)", id); e != nil {
		return e
	}
	if _, e := q.Exec(ctx, `UPDATE outbox o SET payload='{}',state=CASE WHEN state IN ('queued','sending') THEN 'failed' ELSE state END,last_error=CASE WHEN state IN ('queued','sending') THEN 'invitation_revoked' ELSE last_error END,leased_until=NULL FROM employee_invitations i WHERE o.id=i.id::text AND o.provider='smtp_invitation' AND i.employee_id=$1 AND NOT i.revoked`, id); e != nil {
		return e
	}
	_, e := q.Exec(ctx, "UPDATE employee_invitations SET revoked=true,token_hash='' WHERE employee_id=$1 AND NOT revoked", id)
	return e
}

// Legacy token invitations have no employee-invitation record and retain their API.
func validateEmployeeEnrollment(ctx context.Context, q DBTX, tokenHash string) error {
	var known, valid bool
	e := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM employee_invitations WHERE token_hash=$1),EXISTS(SELECT 1 FROM employee_invitations i JOIN employees emp ON emp.id=i.employee_id JOIN users u ON u.id=i.user_id WHERE i.token_hash=$1 AND NOT i.revoked AND i.expires_at>now() AND NOT u.active AND emp.user_id=i.user_id AND emp.provider=i.provider AND emp.external_id=i.external_id AND emp.email=i.email)`, tokenHash).Scan(&known, &valid)
	if e != nil {
		return e
	}
	if known && !valid {
		return denied()
	}
	// Pending directory enrollments cannot activate a known inactive identity.
	var provider, did string
	e = q.QueryRow(ctx, "SELECT u.auth_provider,u.directory_id FROM enrollments e JOIN users u ON u.id=e.user_id WHERE e.hash=$1", tokenHash).Scan(&provider, &did)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	if provider == "ldap" {
		var active bool
		if e = q.QueryRow(ctx, "SELECT active FROM directory_entries WHERE id=$1", did).Scan(&active); e != nil || !active {
			return denied()
		}
	}
	return nil
}
