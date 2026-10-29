package planner

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type enrollment struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	Name         string `json:"name"`
	PasswordHash string `json:"password_hash"`
	Secret       string `json:"secret"`
	Admin        bool   `json:"admin"`
}

func (a *App) beginEnrollment(w http.ResponseWriter, r *http.Request, u Principal) error {
	var in struct {
		Token    string `json:"token"`
		Username string `json:"username"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if e := decode(r, &in); e != nil {
		return e
	}
	if e := a.throttle(r, "enroll:"+hash(in.Token)); e != nil {
		return e
	}
	if len(in.Token) < 32 || len(in.Password) > 1024 || checkText(in.Name, 200, true) != nil {
		return bad("Нужны токен, имя и пароль 15–128 символов")
	}
	in.Username = strings.ToLower(strings.TrimSpace(in.Username))
	if len(in.Username) == 0 || len(in.Username) > 200 {
		return bad("Invalid username")
	}
	ctx := r.Context()
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(789502)"); e != nil {
		return e
	}
	available, e := setupAvailable(ctx, tx)
	if e != nil {
		return e
	}
	setup := available && subtle.ConstantTimeCompare([]byte(hash(in.Token)), []byte(hash(a.Config.Bootstrap))) == 1
	directoryAccount := false
	payload := enrollment{ID: newID(), Username: in.Username, Name: in.Name, Secret: totpSecret(), Admin: setup}
	if !setup {
		var uid string
		e = tx.QueryRow(ctx, "SELECT coalesce(user_id,'') FROM enrollments WHERE hash=$1 AND expires_at>now() FOR UPDATE", hash(in.Token)).Scan(&uid)
		if e != nil || uid == "" {
			return denied()
		}
		var active bool
		var provider string
		e = tx.QueryRow(ctx, "SELECT username,admin,active,auth_provider FROM users WHERE id=$1", uid).Scan(&payload.Username, &payload.Admin, &active, &provider)
		if e != nil || active {
			return denied()
		}
		if e = validateEmployeeEnrollment(ctx, tx, hash(in.Token)); e != nil {
			return e
		}
		payload.ID = uid
		directoryAccount = provider == "ldap"
		if directoryAccount {
			ok, err := a.verifyLoginPassword(ctx, uid, payload.Username, in.Password, "")
			if err != nil || !ok {
				return appError{401, "invalid_login", "Неверные данные входа"}
			}
		}
	}
	if !directoryAccount && !validID.MatchString(payload.Username) {
		return bad("Invalid local username")
	}
	if !directoryAccount && !validPassword(in.Password) {
		return bad("Пароль: 15–128 символов")
	}
	select {
	case a.passwordSlots <- struct{}{}:
		defer func() { <-a.passwordSlots }()
	default:
		return appError{429, "busy", "Повторите позже"}
	}
	var previous []byte
	if err := tx.QueryRow(ctx, "SELECT payload FROM enrollments WHERE hash=$1 AND expires_at>now() AND payload IS NOT NULL", hash(in.Token)).Scan(&previous); err == nil {
		if raw, err := unseal(a.Config.Secret, previous); err == nil {
			var prev enrollment
			if json.Unmarshal(raw, &prev) == nil && prev.Username == payload.Username {
				payload.ID = prev.ID
				payload.Secret = prev.Secret
			}
		}
	}
	if !directoryAccount {
		payload.PasswordHash = passwordHash(in.Password)
	}
	raw, _ := json.Marshal(payload)
	sealed, e := seal(a.Config.Secret, raw)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO enrollments(hash,user_id,expires_at,payload) VALUES($1,$2,now()+interval '15 minutes',$3) ON CONFLICT(hash) DO UPDATE SET payload=excluded.payload,expires_at=excluded.expires_at`, hash(in.Token), func() any {
		if setup {
			return nil
		}
		return payload.ID
	}(), sealed)
	if e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	uri := "otpauth://totp/" + url.PathEscape("GM Vector:"+payload.Username) + "?secret=" + payload.Secret + "&issuer=GM%20Vector&algorithm=SHA1&digits=6&period=30"
	jsonResponse(w, 200, map[string]string{"secret": payload.Secret, "otpauth_uri": uri})
	return nil
}
func (a *App) confirmEnrollment(w http.ResponseWriter, r *http.Request, u Principal) error {
	var in struct {
		Token string `json:"token"`
		Code  string `json:"code"`
	}
	if e := decode(r, &in); e != nil {
		return e
	}
	if e := a.throttle(r, "confirm:"+hash(in.Token)); e != nil {
		return e
	}
	ctx := r.Context()
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(789502)"); e != nil {
		return e
	}
	var sealed []byte
	var uid string
	e = tx.QueryRow(ctx, "SELECT payload,coalesce(user_id,'') FROM enrollments WHERE hash=$1 AND expires_at>now() FOR UPDATE", hash(in.Token)).Scan(&sealed, &uid)
	if e != nil {
		return denied()
	}

	if uid != "" {
		var directoryID string
		if e = tx.QueryRow(ctx, "SELECT directory_id FROM users WHERE id=$1 AND auth_provider='ldap'", uid).Scan(&directoryID); e == nil {
			var active bool
			if a.Config.LDAP == nil || !a.Config.LDAP.Enabled {
				return denied()
			}
			if e = tx.QueryRow(ctx, "SELECT active AND provider=$2 FROM directory_entries WHERE id=$1 FOR SHARE", directoryID, a.Config.LDAP.Provider).Scan(&active); e != nil || !active {
				return denied()
			}
		} else if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
	}
	if e = validateEmployeeEnrollment(ctx, tx, hash(in.Token)); e != nil {
		return e
	}
	raw, e := unseal(a.Config.Secret, sealed)
	if e != nil {
		return denied()
	}
	var p enrollment
	if json.Unmarshal(raw, &p) != nil {
		return denied()
	}
	step := totpStep(p.Secret, in.Code, -1, time.Now())
	if step < 0 {
		return appError{401, "invalid_code", "Код не подошёл. Введите 6 цифр из записи GM Vector в аутентификаторе; проверьте автоматические дату и время на телефоне."}
	}
	available, e := setupAvailable(ctx, tx)
	if e != nil {
		return e
	}
	if uid == "" && (!available || !p.Admin) {
		return denied()
	}
	secret, e := seal(a.Config.Secret, []byte(p.Secret))
	if e != nil {
		return e
	}
	codes, hashes := []string{}, []string{}
	for i := 0; i < 10; i++ {
		code := token()[:20]
		codes = append(codes, code)
		hashes = append(hashes, hash(code))
	}
	if uid == "" {
		_, e = tx.Exec(ctx, "INSERT INTO users(id,username,name,password_hash,totp,last_totp,admin,active,recovery_hashes) VALUES($1,$2,$3,$4,$5,$6,true,true,$7)", p.ID, p.Username, p.Name, p.PasswordHash, secret, step, hashes)
	} else {
		_, e = tx.Exec(ctx, "UPDATE users SET name=$2,password_hash=$3,totp=$4,last_totp=$5,active=true,recovery_hashes=$6 WHERE id=$1 AND NOT active", p.ID, p.Name, p.PasswordHash, secret, step, hashes)
	}
	if e != nil {
		return e
	}
	if uid == "" {
		if _, e = tx.Exec(ctx, "INSERT INTO settings(key,value) VALUES('bootstrap_consumed','true'::jsonb) ON CONFLICT(key) DO NOTHING"); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, "DELETE FROM enrollments WHERE hash=$1", hash(in.Token)); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE outbox o SET payload='{}',state=CASE WHEN o.state IN ('queued','sending') THEN 'failed' ELSE o.state END,last_error=CASE WHEN o.state IN ('queued','sending') THEN 'account_activated' ELSE o.last_error END FROM employee_invitations i WHERE o.id=i.id::text AND o.provider='smtp_invitation' AND i.user_id=$1", p.ID); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE employee_invitations SET token_hash='' WHERE user_id=$1", p.ID); e != nil {
		return e
	}
	if e = audit(ctx, tx, p.ID, "", "auth.enrolled", requestID(r), map[string]bool{"bootstrap": uid == ""}); e != nil {
		return e
	}
	if e = a.session(ctx, tx, w, p.ID); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	jsonResponse(w, 200, map[string]any{"recovery_codes": codes})
	return nil
}
func (a *App) login(w http.ResponseWriter, r *http.Request, u Principal) error {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if e := decode(r, &in); e != nil {
		return e
	}
	if len(in.Username) > 200 || len(in.Password) > 1024 || len(in.Code) > 128 {
		return denied()
	}
	in.Username = strings.ToLower(strings.TrimSpace(in.Username))
	if e := a.throttle(r, in.Username); e != nil {
		return e
	}
	select {
	case a.passwordSlots <- struct{}{}:
		defer func() { <-a.passwordSlots }()
	default:
		return appError{429, "busy", "Повторите позже"}
	}
	ctx := r.Context()
	var uid, ph string
	var secret []byte
	var active bool
	e := a.Store.DB.QueryRow(ctx, "SELECT id,password_hash,totp,active FROM users WHERE username=$1", in.Username).Scan(&uid, &ph, &secret, &active)
	if e != nil {
		passwordOK(in.Password, a.dummyHash)
		return appError{401, "invalid_login", "Неверные данные входа"}
	}
	valid, verifyErr := a.verifyLoginPassword(ctx, uid, in.Username, in.Password, ph)
	if verifyErr != nil || !valid || !active {
		return appError{401, "invalid_login", "Неверные данные входа"}
	}
	plain, e := unseal(a.Config.Secret, secret)
	if e != nil {
		return appError{401, "invalid_login", "Неверные данные входа"}
	}
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var last int64
	var recovery []string
	e = tx.QueryRow(ctx, "SELECT last_totp,recovery_hashes FROM users WHERE id=$1 AND active FOR UPDATE", uid).Scan(&last, &recovery)
	if e != nil {
		return denied()
	}
	step := totpStep(string(plain), in.Code, last, time.Now())
	usedRecovery := false
	if step < 0 {
		found := -1
		for i, h := range recovery {
			if subtle.ConstantTimeCompare([]byte(h), []byte(hash(in.Code))) == 1 {
				found = i
				break
			}
		}
		if found < 0 {
			return appError{401, "invalid_login", "Неверные данные входа"}
		}
		recovery = append(recovery[:found], recovery[found+1:]...)
		usedRecovery = true
		step = last
	}
	if _, e = tx.Exec(ctx, "UPDATE users SET last_totp=$2,recovery_hashes=$3 WHERE id=$1", uid, step, recovery); e != nil {
		return e
	}
	if e = audit(ctx, tx, uid, "", "auth.login", requestID(r), map[string]bool{"recovery": usedRecovery}); e != nil {
		return e
	}
	if e = a.session(ctx, tx, w, uid); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
	return nil
}
func (a *App) changePassword(w http.ResponseWriter, r *http.Request, u Principal) error {
	var in struct {
		Old  string `json:"old_password"`
		New  string `json:"new_password"`
		Code string `json:"code"`
	}
	if e := decode(r, &in); e != nil {
		return e
	}
	if !validPassword(in.New) {
		return bad("Пароль: 15–128 символов")
	}
	if e := a.throttle(r, "change:"+u.ID); e != nil {
		return e
	}
	select {
	case a.passwordSlots <- struct{}{}:
		defer func() { <-a.passwordSlots }()
	default:
		return appError{429, "rate_limit", "Повторите позже"}
	}
	ctx := r.Context()
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var ph string
	var secret []byte
	var last int64
	if e = tx.QueryRow(ctx, "SELECT password_hash,totp,last_totp FROM users WHERE id=$1 FOR UPDATE", u.ID).Scan(&ph, &secret, &last); e != nil {
		return e
	}
	plain, e := unseal(a.Config.Secret, secret)
	if e != nil {
		return e
	}
	step := totpStep(string(plain), in.Code, last, time.Now())
	if step < 0 || !passwordOK(in.Old, ph) {
		return denied()
	}
	if _, e = tx.Exec(ctx, "UPDATE users SET password_hash=$2,last_totp=$3 WHERE id=$1", u.ID, passwordHash(in.New), step); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "DELETE FROM sessions WHERE user_id=$1", u.ID); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "DELETE FROM api_tokens WHERE user_id=$1", u.ID); e != nil {
		return e
	}
	if e = audit(ctx, tx, u.ID, "", "auth.password_changed", requestID(r), nil); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
	return nil
}
func (a *App) users(w http.ResponseWriter, r *http.Request, u Principal) error {
	if !u.Admin {
		return denied()
	}
	rows, e := a.Store.DB.Query(r.Context(), "SELECT id,username,name,admin,active FROM users ORDER BY username")
	if e != nil {
		return e
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var v User
		if e = rows.Scan(&v.ID, &v.Username, &v.Name, &v.Admin, &v.Active); e != nil {
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
func (a *App) inviteUser(w http.ResponseWriter, r *http.Request, u Principal) error {
	if !u.Admin {
		return denied()
	}
	var in struct {
		Username string `json:"username"`
		Name     string `json:"name"`
		Admin    bool   `json:"admin"`
	}
	if e := decode(r, &in); e != nil {
		return e
	}
	in.Username = strings.ToLower(in.Username)
	if !validID.MatchString(in.Username) || checkText(in.Name, 200, true) != nil {
		return bad("Проверьте логин и имя")
	}
	ctx := r.Context()
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	uid, raw := newID(), token()
	if _, e = tx.Exec(ctx, "INSERT INTO users(id,username,name,password_hash,totp,admin) VALUES($1,$2,$3,'',$4,$5)", uid, in.Username, in.Name, []byte{}, in.Admin); e != nil {
		return bad("Логин уже занят")
	}
	if _, e = tx.Exec(ctx, "INSERT INTO enrollments(hash,user_id,expires_at) VALUES($1,$2,now()+interval '24 hours')", hash(raw), uid); e != nil {
		return e
	}
	if e = audit(ctx, tx, u.ID, "", "user.invited", requestID(r), map[string]string{"user_id": uid}); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	jsonResponse(w, 201, map[string]string{"id": uid, "enrollment_token": raw})
	return nil
}
func (a *App) updateUser(w http.ResponseWriter, r *http.Request, u Principal) error {
	if !u.Admin {
		return denied()
	}
	var in struct {
		Active bool `json:"active"`
	}
	if e := decode(r, &in); e != nil {
		return e
	}
	id := r.PathValue("id")
	if id == u.ID {
		return bad("Нельзя заблокировать текущего администратора")
	}
	ctx := r.Context()
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(789502)"); e != nil {
		return e
	}
	if in.Active {
		var totp []byte
		var provider, directoryID string
		if e = tx.QueryRow(ctx, "SELECT totp,auth_provider,directory_id FROM users WHERE id=$1 FOR UPDATE", id).Scan(&totp, &provider, &directoryID); e != nil {
			return e
		}
		if len(totp) == 0 {
			return bad("Сначала завершите регистрацию MFA")
		}
		if provider == "ldap" {
			if a.Config.LDAP == nil || !a.Config.LDAP.Enabled {
				return bad("Directory authentication unavailable")
			}
			var allowed bool
			if e = tx.QueryRow(ctx, "SELECT active AND provider=$2 FROM directory_entries WHERE id=$1", directoryID, a.Config.LDAP.Provider).Scan(&allowed); e != nil || !allowed {
				return bad("Directory account is not active")
			}
		} else if provider != "local" {
			return denied()
		}

	}
	if _, e = tx.Exec(ctx, "UPDATE users SET active=$2 WHERE id=$1", id, in.Active); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "DELETE FROM sessions WHERE user_id=$1", id); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "DELETE FROM api_tokens WHERE user_id=$1", id); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "DELETE FROM enrollments WHERE user_id=$1", id); e != nil {
		return e
	}
	var employeeID string
	if err := tx.QueryRow(ctx, "SELECT id FROM employees WHERE user_id=$1", id).Scan(&employeeID); err == nil {
		if e = revokeEmployeeInvites(ctx, tx, employeeID); e != nil {
			return e
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if e = audit(ctx, tx, u.ID, "", "user.active", requestID(r), map[string]any{"user_id": id, "active": in.Active}); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
	return nil
}

var _ = errors.Is
var _ = pgx.ErrNoRows

// A still-valid setup token can resume only its own unfinished enrollment.
// It does not create a session, activate an account, or return a password hash.
func (a *App) resumeEnrollment(w http.ResponseWriter, r *http.Request, u Principal) error {
	var in struct {
		Token string `json:"token"`
	}
	if e := decode(r, &in); e != nil {
		return e
	}
	if len(in.Token) < 32 {
		return denied()
	}
	if e := a.throttle(r, "resume:"+hash(in.Token)); e != nil {
		return e
	}
	if subtle.ConstantTimeCompare([]byte(hash(in.Token)), []byte(hash(a.Config.Bootstrap))) == 1 {
		available, e := setupAvailable(r.Context(), a.Store.DB)
		if e != nil {
			return e
		}
		if !available {
			return denied()
		}
	}
	var encrypted []byte
	if e := a.Store.DB.QueryRow(r.Context(), "SELECT payload FROM enrollments WHERE hash=$1 AND expires_at>now() AND payload IS NOT NULL", hash(in.Token)).Scan(&encrypted); e != nil {
		return denied()
	}
	if e := validateEmployeeEnrollment(r.Context(), a.Store.DB, hash(in.Token)); e != nil {
		return e
	}
	plain, e := unseal(a.Config.Secret, encrypted)
	if e != nil {
		return denied()
	}
	var p enrollment
	if json.Unmarshal(plain, &p) != nil {
		return denied()
	}
	uri := "otpauth://totp/" + url.PathEscape("GM Vector:"+p.Username) + "?secret=" + p.Secret + "&issuer=GM%20Vector&algorithm=SHA1&digits=6&period=30"
	jsonResponse(w, 200, map[string]string{"secret": p.Secret, "otpauth_uri": uri, "username": p.Username})
	return nil
}

func (a *App) preferences(w http.ResponseWriter, r *http.Request, u Principal) error {
	if u.Token {
		return denied()
	}
	var in struct {
		Locale string `json:"locale"`
	}
	if e := decode(r, &in); e != nil {
		return e
	}
	if !contains([]string{"en", "ru", "de", "fr", "es", "pt-BR", "zh-Hans"}, in.Locale) {
		return bad("Unsupported locale")
	}
	if _, e := a.Store.DB.Exec(r.Context(), "UPDATE users SET locale=$2 WHERE id=$1", u.ID, in.Locale); e != nil {
		return e
	}
	jsonResponse(w, 200, map[string]string{"locale": in.Locale})
	return nil
}
