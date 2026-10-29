package planner

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var Version = "0.1.0"

var ReleaseDate = time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)

type Config struct {
	UpdaterSocket   string
	UpdaterToken    string
	TrustedProxies  []netip.Prefix
	ReportPublicKey *ReportPublicKey
	ReportDirectory string
	Origin          string
	WebDir          string
	Secret          []byte
	Bootstrap       string // Deprecated: ignored; first-run setup no longer uses an installation key.
	LDAP            *LDAPConfig
	SMTP            SMTPConfig
}
type App struct {
	diagnosticEvents diagnosticBuffer
	Store            *Store
	Config           Config
	dummyHash        string
	passwordSlots    chan struct{}
	directoryLookup  func(context.Context, Employee) (DirectoryPerson, error)
}
type appError struct {
	Status        int
	Code, Message string
}

func (e appError) Error() string { return e.Message }
func bad(s string) error         { return appError{422, "validation", s} }
func denied() error              { return appError{403, "forbidden", "Недостаточно прав"} }
func conflict() error {
	return appError{409, "revision_conflict", "Проект изменён другим пользователем; обновите данные"}
}

type handler func(http.ResponseWriter, *http.Request, Principal) error

func jsonResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func decode(r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return appError{415, "content_type", "Требуется application/json"}
	}
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(e, &tooLarge) {
			return appError{413, "too_large", "Файл больше 5 MiB"}
		}
		return bad("Неверный JSON или неизвестные поля")
	}
	if e := d.Decode(&struct{}{}); e != io.EOF {
		return bad("Лишние данные JSON")
	}
	return nil
}
func requestID(r *http.Request) string { s, _ := r.Context().Value("requestID").(string); return s }
func NewApp(s *Store, c Config) (*App, error) {
	u, e := url.Parse(c.Origin)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(c.Secret) != 32 {
		return nil, errors.New("invalid secure configuration")
	}
	if e := validateSMTP(c.SMTP); e != nil {
		return nil, e
	}
	a := &App{Store: s, Config: c, dummyHash: passwordHash(token()), passwordSlots: make(chan struct{}, 4)}
	return a, nil
}
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { jsonResponse(w, 200, map[string]bool{"ok": true}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if a.Store.DB.Ping(ctx) != nil {
			jsonResponse(w, 503, map[string]bool{"ready": false})
			return
		}
		jsonResponse(w, 200, map[string]bool{"ready": true})
	})
	routes := map[string]handler{
		"GET /api/v1/admin/updates": a.updaterStatus, "POST /api/v1/admin/updates/check": a.updaterCheck, "POST /api/v1/admin/updates/apply": a.updaterApply, "POST /api/v1/admin/updates/recover": a.updaterRecover,
		"GET /api/v1/projects/{id}/tasks/{task}/comments": a.taskComments, "POST /api/v1/projects/{id}/tasks/{task}/comments": a.addTaskComment,
		"GET /api/v1/auth/me": a.me, "POST /api/v1/auth/logout": a.logout, "POST /api/v1/auth/password": a.changePassword, "PUT /api/v1/auth/preferences": a.preferences,
		"GET /api/v1/projects": a.projects, "POST /api/v1/projects": a.createProject, "GET /api/v1/projects/{id}": a.getProject, "PUT /api/v1/projects/{id}": a.saveProject, "DELETE /api/v1/projects/{id}": a.archiveProject, "POST /api/v1/projects/{id}/preview": a.previewProject, "POST /api/v1/projects/{id}/undo": a.undoProject,
		"GET /api/v1/employees": a.employees, "POST /api/v1/employees": a.saveEmployee, "POST /api/v1/employees/{id}/invite": a.inviteEmployee, "GET /api/v1/employees/{id}/invite": a.employeeInvite, "GET /api/v1/calendars": a.calendars, "POST /api/v1/calendars": a.importCalendar,
		"GET /api/v1/integrations": a.integrations, "GET /api/v1/diagnostics": a.diagnostics, "GET /api/v1/diagnostics/events": a.getDiagnosticEvents, "GET /api/v1/audit": a.auditLog,
		"GET /api/v1/trash": a.trash, "POST /api/v1/trash/{id}/restore": a.restoreProject,
		"GET /api/v1/projects/{id}/member-candidates": a.memberCandidates,
		"GET /api/v1/users":                           a.users, "POST /api/v1/users": a.inviteUser, "PATCH /api/v1/users/{id}": a.updateUser,
		"GET /api/v1/projects/{id}/members": a.members, "PUT /api/v1/projects/{id}/members": a.setMember,
		"GET /api/v1/projects/{id}/time-entries": a.timeEntries, "POST /api/v1/projects/{id}/time-entries": a.addTimeEntry, "DELETE /api/v1/projects/{id}/time-entries/{entry}": a.deleteTimeEntry,
		"GET /api/v1/projects/{id}/export": a.exportProject, "POST /api/v1/import": a.importProject,
		"GET /api/v1/tokens": a.tokens, "POST /api/v1/tokens": a.createToken, "DELETE /api/v1/tokens/{id}": a.revokeToken,
		"GET /api/v1/directory/people": a.directoryPeople, "POST /api/v1/directory/sync": a.syncDirectory, "GET /api/v1/directory/status": a.directoryStatus, "POST /api/v1/directory/ldap/sync": a.syncLDAPDirectory, "GET /api/v1/openapi.json": a.apiDocs,
	}
	for pattern, h := range routes {
		mux.HandleFunc(pattern, a.wrap(h, true))
	}
	mux.HandleFunc("GET /api/v1/diagnostics/key", a.wrap(a.reportKey, false))
	mux.HandleFunc("POST /api/v1/diagnostics/reports", a.wrap(a.saveBugReport, false))
	mux.HandleFunc("GET /api/v1/auth/status", a.wrap(a.authStatus, false))
	mux.HandleFunc("POST /api/v1/auth/login", a.wrap(a.login, false))
	mux.HandleFunc("POST /api/v1/auth/bootstrap", a.wrap(a.beginEnrollment, false))
	mux.HandleFunc("POST /api/v1/auth/bootstrap/resume", a.wrap(a.resumeEnrollment, false))
	mux.HandleFunc("POST /api/v1/auth/bootstrap/confirm", a.wrap(a.confirmEnrollment, false))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 404, map[string]any{"error": map[string]string{"code": "not_found", "message": "Маршрут не найден"}})
	})
	fs := http.FileServer(http.Dir(a.Config.WebDir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "method not allowed", 405)
			return
		}
		p := filepath.Clean(r.URL.Path)
		if strings.HasSuffix(p, ".map") || p == "/openapi.json" || strings.Contains(p, "/.") {
			http.NotFound(w, r)
			return
		}
		if p == "/" {
			http.ServeFile(w, r, filepath.Join(a.Config.WebDir, "index.html"))
			return
		}
		fs.ServeHTTP(w, r)
	})
	origin, _ := url.Parse(a.Config.Origin)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		state := &diagnosticRequest{}
		r = r.WithContext(context.WithValue(r.Context(), diagnosticContextKey{}, state))
		response := &diagnosticResponse{ResponseWriter: w}
		w = response
		defer func() {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				a.diagnosticEvents.add(DiagnosticEvent{Time: started.UTC(), RequestID: requestID(r), Route: safeDiagnosticRoute(r.Pattern), Method: safeDiagnosticMethod(r.Method), Status: response.statusCode(), DurationMS: time.Since(started).Milliseconds()}, state.owner)
			}
		}()
		id := newID()
		r = r.WithContext(context.WithValue(r.Context(), "requestID", id))
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'; form-action 'self'")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if r.URL.Path != "/healthz" && r.URL.Path != "/readyz" && r.Host != origin.Host {
			http.Error(w, "invalid host", 400)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
		defer func() {
			if recover() != nil {
				slog.Error("request panic", "request_id", id)
				jsonResponse(w, 500, map[string]any{"error": map[string]string{"code": "internal", "message": "Внутренняя ошибка"}, "request_id": id})
			}
		}()
		mux.ServeHTTP(w, r)
	})
}
func (a *App) wrap(h handler, auth bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var u Principal
		var e error
		if auth {
			u, e = a.authenticate(r)
			if e == nil && !u.Token {
				if state, ok := r.Context().Value(diagnosticContextKey{}).(*diagnosticRequest); ok {
					state.owner = u.ID
				}
			}
		}
		if e == nil && r.Method != "GET" && r.Method != "HEAD" {
			if !u.Token && r.Header.Get("Origin") != a.Config.Origin {
				e = denied()
			}
			if auth && !u.Token && r.Header.Get("X-CSRF-Token") != u.CSRF {
				e = denied()
			}
		}
		if e == nil && u.Token {
			scope, allowed := "", false
			switch r.Method + " " + r.URL.Path {
			case "GET /api/v1/directory/people":
				scope, allowed = "directory:read", true
			case "POST /api/v1/directory/sync":
				scope, allowed = "directory:write", true
			default:
				if strings.HasPrefix(r.URL.Path, "/api/v1/projects/") || r.URL.Path == "/api/v1/projects" {
					allowed = true
					scope = "project:read"
					if r.Method != "GET" {
						scope = "project:write"
					}
					if strings.HasSuffix(r.URL.Path, "/export") {
						scope = "export"
					}
					if strings.HasSuffix(r.URL.Path, "/members") || strings.HasSuffix(r.URL.Path, "/member-candidates") {
						allowed = false
					}
				}
			}
			if !allowed || !contains(u.Scopes, scope) {
				e = denied()
			}
		}
		if e == nil {
			e = h(w, r, u)
		}
		if e == nil {
			return
		}
		var ae appError
		if !errors.As(e, &ae) {
			ae = appError{500, "internal", "Внутренняя ошибка"}
			if errors.Is(e, pgx.ErrNoRows) {
				ae = appError{404, "not_found", "Объект не найден"}
			} else {
				slog.Error("request failed", "request_id", requestID(r), "type", fmtErrorType(e))
			}
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/auth/") && r.Method == "POST" {
			_ = audit(r.Context(), a.Store.DB, "", "", "auth.rejected", requestID(r), map[string]any{"route": r.URL.Path, "status": ae.Status})
		}
		jsonResponse(w, ae.Status, map[string]any{"error": map[string]string{"code": ae.Code, "message": ae.Message}, "request_id": requestID(r)})
	}
}
func fmtErrorType(e error) string { return "operation_failed" }

func (a *App) authenticate(r *http.Request) (Principal, error) {
	var u Principal
	var sid string
	if raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "); raw != r.Header.Get("Authorization") && raw != "" {
		u.Token = true
		e := a.Store.DB.QueryRow(r.Context(), `SELECT u.id,u.username,u.name,u.admin,u.active,coalesce(e.id,''),t.project_ids,t.scopes FROM api_tokens t JOIN users u ON u.id=t.user_id LEFT JOIN employees e ON e.user_id=u.id WHERE t.hash=$1 AND t.expires_at>now() AND u.active`, hash(raw)).Scan(&u.ID, &u.Username, &u.Name, &u.Admin, &u.Active, &u.EmployeeID, &u.Projects, &u.Scopes)
		if e != nil {
			return u, appError{401, "unauthorized", "Требуется вход"}
		}
		return u, nil
	}
	c, e := r.Cookie("__Host-gma")
	if e == nil {
		sid = c.Value
	}
	if sid == "" {
		return u, appError{401, "unauthorized", "Требуется вход"}
	}
	e = a.Store.DB.QueryRow(r.Context(), `SELECT u.id,u.username,u.name,u.admin,u.active,coalesce(e.id,''),s.csrf FROM sessions s JOIN users u ON u.id=s.user_id LEFT JOIN employees e ON e.user_id=u.id WHERE s.hash=$1 AND u.active AND s.created_at>now()-interval '12 hours' AND s.seen_at>now()-interval '30 minutes'`, hash(sid)).Scan(&u.ID, &u.Username, &u.Name, &u.Admin, &u.Active, &u.EmployeeID, &u.CSRF)
	if e != nil {
		return u, appError{401, "unauthorized", "Сессия завершена"}
	}
	_, e = a.Store.DB.Exec(r.Context(), "UPDATE sessions SET seen_at=now() WHERE hash=$1", hash(sid))
	return u, e
}
func (a *App) session(ctx context.Context, tx pgx.Tx, w http.ResponseWriter, uid string) error {
	raw, csrf := token(), token()
	_, e := tx.Exec(ctx, "INSERT INTO sessions(hash,user_id,csrf) VALUES($1,$2,$3)", hash(raw), uid, csrf)
	if e != nil {
		return e
	}
	http.SetCookie(w, &http.Cookie{Name: "__Host-gma", Value: raw, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	return nil
}
func (a *App) throttle(r *http.Request, username string) error {
	ip, err := a.loginClientIP(r)
	if err != nil {
		return err
	}
	for _, v := range []struct {
		k   string
		max int
	}{{"ip:" + ip, 100}, {"account:" + strings.ToLower(username), 10}} {
		var n int
		e := a.Store.DB.QueryRow(r.Context(), `INSERT INTO login_limits(key,attempts) VALUES($1,1) ON CONFLICT(key) DO UPDATE SET attempts=CASE WHEN login_limits.window_start<now()-interval '15 minutes' THEN 1 ELSE login_limits.attempts+1 END,window_start=CASE WHEN login_limits.window_start<now()-interval '15 minutes' THEN now() ELSE login_limits.window_start END RETURNING attempts`, hash(v.k)).Scan(&n)
		if e != nil {
			return e
		}
		if n > v.max {
			return appError{429, "rate_limit", "Слишком много попыток. Повторите через 15 минут"}
		}
	}
	return nil
}
func (a *App) requireProject(r *http.Request, u Principal, write bool) (string, error) {
	role, e := a.Store.role(r.Context(), u, r.PathValue("id"))
	if e != nil {
		return "", appError{404, "not_found", "Проект не найден"}
	}
	if write && role == "viewer" {
		return "", denied()
	}
	return role, nil
}
func (a *App) me(w http.ResponseWriter, r *http.Request, u Principal) error {
	var locale string
	if e := a.Store.DB.QueryRow(r.Context(), "SELECT locale FROM users WHERE id=$1", u.ID).Scan(&locale); e != nil {
		return e
	}
	jsonResponse(w, 200, map[string]any{"user": struct {
		User
		Locale string `json:"locale"`
	}{u.User, locale}, "csrf": u.CSRF, "version": Version})
	return nil
}
func (a *App) authStatus(w http.ResponseWriter, r *http.Request, u Principal) error {
	available, e := setupAvailable(r.Context(), a.Store.DB)
	if e != nil {
		return e
	}
	result := map[string]any{"setup_required": available, "version": Version}
	jsonResponse(w, 200, result)
	return nil
}
func (a *App) logout(w http.ResponseWriter, r *http.Request, u Principal) error {
	if c, e := r.Cookie("__Host-gma"); e == nil {
		if _, e = a.Store.DB.Exec(r.Context(), "DELETE FROM sessions WHERE hash=$1", hash(c.Value)); e != nil {
			return e
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "__Host-gma", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	jsonResponse(w, 200, map[string]bool{"ok": true})
	return nil
}
func (a *App) apiDocs(w http.ResponseWriter, r *http.Request, u Principal) error {
	b, e := os.ReadFile(filepath.Join(a.Config.WebDir, "openapi.json"))
	if e != nil {
		return e
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
	return nil
}
