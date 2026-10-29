package planner

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/url"
)

func (a *App) invitationMailTick(ctx context.Context) error {
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(789502)"); e != nil {
		return e
	}
	// Clean secrets even when SMTP is disabled. Never replay an ambiguous lease.
	if _, e = tx.Exec(ctx, `UPDATE outbox SET state='uncertain',payload='{}',last_error='delivery_interrupted',leased_until=NULL WHERE provider='smtp_invitation' AND state='sending' AND leased_until<now()`); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE outbox o SET payload='{}',state=CASE WHEN o.state IN ('queued','sending') THEN 'failed' ELSE o.state END,last_error=CASE WHEN o.state IN ('queued','sending') THEN 'invitation_expired_or_revoked' ELSE o.last_error END,leased_until=NULL FROM employee_invitations i JOIN employees emp ON emp.id=i.employee_id JOIN users u ON u.id=i.user_id WHERE o.id=i.id::text AND o.provider='smtp_invitation' AND (i.revoked OR i.expires_at<=now() OR u.active OR emp.user_id IS DISTINCT FROM i.user_id OR emp.provider<>i.provider OR emp.external_id<>i.external_id OR emp.email<>i.email OR NOT EXISTS(SELECT 1 FROM enrollments e WHERE e.hash=i.token_hash AND e.expires_at>now()))`); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `DELETE FROM enrollments WHERE hash IN (SELECT token_hash FROM employee_invitations WHERE revoked OR expires_at<=now())`); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE employee_invitations SET token_hash='' WHERE revoked OR expires_at<=now()`); e != nil {
		return e
	}
	if !a.Config.SMTP.Enabled {
		return tx.Commit(ctx)
	}
	var id string
	e = tx.QueryRow(ctx, "SELECT id FROM outbox WHERE provider='smtp_invitation' AND state='queued' AND ready_at<=now() ORDER BY ready_at,id FOR UPDATE SKIP LOCKED LIMIT 1").Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE outbox SET state='sending',attempts=attempts+1,leased_until=now()+interval '60 seconds' WHERE id=$1", id); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	return a.deliverInvitation(ctx, id)
}
func (a *App) deliverInvitation(ctx context.Context, id string) error {
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	// Serializes enrollment, reissue and source/employee changes during bounded SMTP send.
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(789502)"); e != nil {
		return e
	}
	var state string
	var raw []byte
	if e = tx.QueryRow(ctx, "SELECT state,payload FROM outbox WHERE id=$1 AND provider='smtp_invitation' FOR UPDATE", id).Scan(&state, &raw); e != nil {
		return e
	}
	if state != "sending" {
		return nil
	}
	finish := func(state, reason string) error {
		if _, e := tx.Exec(ctx, "UPDATE outbox SET state=$2,last_error=$3,payload='{}',leased_until=NULL WHERE id=$1", id, state, reason); e != nil {
			return e
		}
		return tx.Commit(ctx)
	}
	var eligible bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM employee_invitations i JOIN employees emp ON emp.id=i.employee_id JOIN users u ON u.id=i.user_id JOIN enrollments e ON e.hash=i.token_hash WHERE i.id=$1 AND NOT i.revoked AND i.expires_at>now() AND e.expires_at>now() AND NOT u.active AND emp.user_id=i.user_id AND emp.provider=i.provider AND emp.external_id=i.external_id AND emp.email=i.email)`, id).Scan(&eligible); e != nil {
		return e
	}
	if !eligible {
		return finish("failed", "invitation_expired_or_revoked")
	}
	inv, e := readInvite(ctx, tx, id)
	if e != nil {
		return e
	}
	var payload struct {
		Secret []byte `json:"secret"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return finish("failed", "invalid_invitation_payload")
	}
	secret, e := unseal(a.Config.Secret, payload.Secret)
	if e != nil {
		return finish("failed", "invalid_invitation_payload")
	}
	if validateSMTP(a.Config.SMTP) != nil || !validEmail(inv.Email) {
		return finish("failed", "invalid_mail_configuration")
	}
	instruction := "Create your password and configure TOTP using the one-time link:"
	var authProvider string
	if e = tx.QueryRow(ctx, "SELECT auth_provider FROM users WHERE id=$1", inv.UserID).Scan(&authProvider); e != nil {
		return e
	}
	if authProvider == "ldap" {
		instruction = "Use your corporate password and configure TOTP using the one-time link:"
	}
	body := "gmVector\nLogin: " + inv.Username + "\n" + instruction + "\n" + a.Config.Origin + "/#setup=" + url.QueryEscape(string(secret)) + "&login=" + url.QueryEscape(inv.Username) + "\nСсылка действует до: " + inv.ExpiresAt.String() + "\nНе пересылайте приглашение. Доступ к системе открывается только после подтверждения TOTP. Права на проекты выдаёт руководитель отдельно.\n"
	state, reason := sendSMTPMessage(ctx, a.Config.SMTP, inv.Email, smtpMessage(a.Config.SMTP, inv.ID, inv.Email, "gmVector invitation", body))
	return finish(state, reason)
}
