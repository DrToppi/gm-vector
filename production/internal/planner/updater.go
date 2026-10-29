package planner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const updaterMaxResponse = 16 << 10

var updaterCommit = regexp.MustCompile(`^[a-f0-9]{40}$`)
var updaterOperation = regexp.MustCompile(`^[a-f0-9]{32}$`)
var updaterPhases = map[string]bool{"queued": true, "fetch": true, "build": true, "preflight": true, "backup": true, "migrate": true, "verify": true, "restore": true, "complete": true}
var updaterToken = regexp.MustCompile(`^[A-Za-z0-9_-]{32,128}$`)

// Git refs are data, never command options or arbitrary revision expressions.
var updaterRef = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,120}$`)

// UpdaterStatus is the entire browser-visible host-service response. Unknown
// fields, logs, paths and service-provided message text are never relayed.
type UpdaterStatus struct {
	Enabled           bool   `json:"enabled"`
	State             string `json:"state"`
	Phase             string `json:"phase,omitempty"`
	Message           string `json:"message,omitempty"`
	CurrentCommit     string `json:"current_commit,omitempty"`
	CandidateCommit   string `json:"candidate_commit,omitempty"`
	OperationID       string `json:"operation_id,omitempty"`
	RecoveryAvailable bool   `json:"recovery_available"`
}

func loadUpdater(socket, tokenFile string) (string, string, error) {
	if socket == "" && tokenFile == "" {
		return "", "", nil
	}
	if socket == "" || tokenFile == "" || !filepath.IsAbs(socket) || filepath.Clean(socket) != socket || len(socket) > 103 || !filepath.IsAbs(tokenFile) {
		return "", "", errors.New("invalid updater configuration")
	}
	info, err := os.Lstat(tokenFile)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 128 {
		return "", "", errors.New("invalid updater credential file")
	}
	raw, err := os.ReadFile(tokenFile)
	if err != nil || !updaterToken.Match(raw) {
		return "", "", errors.New("invalid updater credential")
	}
	return socket, string(raw), nil
}

func updaterUnavailable() error {
	return appError{503, "updater_unavailable", "Update service is unavailable"}
}

func safeUpdaterStatus(value UpdaterStatus) (UpdaterStatus, error) {
	messages := map[string]string{
		"idle": "Ready to check for updates", "checking": "Checking update source", "ready": "Update is ready for confirmation",
		"updating": "Update in progress", "succeeded": "Update completed", "failed": "Update failed",
		"recovery_required": "Recovery is required", "recovering": "Recovery in progress",
	}
	message, ok := messages[value.State]
	if !value.Enabled || !ok || (value.Phase != "" && !updaterPhases[value.Phase]) ||
		(value.CurrentCommit != "" && !updaterCommit.MatchString(value.CurrentCommit)) ||
		(value.CandidateCommit != "" && !updaterCommit.MatchString(value.CandidateCommit)) ||
		(value.OperationID != "" && !updaterOperation.MatchString(value.OperationID)) {
		return UpdaterStatus{}, updaterUnavailable()
	}
	value.Message = message
	return value, nil
}

func (a *App) updaterCall(r *http.Request, method, endpoint string, input any) (UpdaterStatus, int, error) {
	if a.Config.UpdaterSocket == "" || a.Config.UpdaterToken == "" {
		return UpdaterStatus{}, 0, updaterUnavailable()
	}
	var body io.Reader
	if input != nil {
		raw, _ := json.Marshal(input)
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(r.Context(), method, "http://updater"+endpoint, body)
	if err != nil {
		return UpdaterStatus{}, 0, updaterUnavailable()
	}
	req.Header.Set("Authorization", "Bearer "+a.Config.UpdaterToken)
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", a.Config.UpdaterSocket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return UpdaterStatus{}, 0, updaterUnavailable()
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK, http.StatusAccepted:
	case http.StatusConflict:
		return UpdaterStatus{}, 0, appError{409, "updater_conflict", "Update state changed or another operation is active"}
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return UpdaterStatus{}, 0, bad("Update request was rejected")
	default:
		return UpdaterStatus{}, 0, updaterUnavailable()
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, updaterMaxResponse+1))
	if err != nil || len(raw) > updaterMaxResponse {
		return UpdaterStatus{}, 0, updaterUnavailable()
	}
	var result UpdaterStatus
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if decoder.Decode(&result) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return UpdaterStatus{}, 0, updaterUnavailable()
	}
	result, err = safeUpdaterStatus(result)
	return result, response.StatusCode, err
}

func updaterAdmin(u Principal) error {
	if !u.Admin || u.Token {
		return denied()
	}
	return nil
}

// Record the named administrator request before dispatch. A host job may be
// irreversible once accepted; an audit failure must prevent starting it rather
// than falsely report failure after the job has already started.
func (a *App) updaterDispatch(r *http.Request, u Principal, action, endpoint string, details, input any) (UpdaterStatus, int, error) {
	if a.Config.UpdaterSocket == "" || a.Config.UpdaterToken == "" {
		return UpdaterStatus{}, 0, updaterUnavailable()
	}
	if a.Store == nil || a.Store.DB == nil {
		return UpdaterStatus{}, 0, errors.New("update audit unavailable")
	}
	if err := audit(r.Context(), a.Store.DB, u.ID, "", "updater."+action+".requested", requestID(r), details); err != nil {
		return UpdaterStatus{}, 0, err
	}
	return a.updaterCall(r, "POST", endpoint, input)
}

func (a *App) updaterStatus(w http.ResponseWriter, r *http.Request, u Principal) error {
	if err := updaterAdmin(u); err != nil {
		return err
	}
	if a.Config.UpdaterSocket == "" && a.Config.UpdaterToken == "" {
		jsonResponse(w, 200, UpdaterStatus{Enabled: false, State: "disabled"})
		return nil
	}
	result, status, err := a.updaterCall(r, "GET", "/status", nil)
	if err != nil {
		return err
	}
	jsonResponse(w, status, result)
	return nil
}
func (a *App) updaterCheck(w http.ResponseWriter, r *http.Request, u Principal) error {
	if err := updaterAdmin(u); err != nil {
		return err
	}
	var input struct {
		Ref string `json:"ref"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if err := decode(r, &input); err != nil {
		return err
	}
	if !updaterRef.MatchString(input.Ref) || strings.Contains(input.Ref, "..") || strings.Contains(input.Ref, "//") || strings.Contains(input.Ref, "@{") || strings.HasSuffix(input.Ref, "/") || strings.HasSuffix(input.Ref, ".") || strings.HasSuffix(input.Ref, ".lock") {
		return bad("Invalid update ref")
	}
	result, status, err := a.updaterDispatch(r, u, "check", "/check", struct {
		Ref string `json:"ref"`
	}{input.Ref}, input)
	if err != nil {
		return err
	}
	jsonResponse(w, status, result)
	return nil
}
func (a *App) updaterApply(w http.ResponseWriter, r *http.Request, u Principal) error {
	if err := updaterAdmin(u); err != nil {
		return err
	}
	var input struct {
		Commit       string `json:"commit"`
		Confirmation string `json:"confirmation"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if err := decode(r, &input); err != nil {
		return err
	}
	if !updaterCommit.MatchString(input.Commit) || input.Confirmation != input.Commit {
		return bad("Confirm the exact candidate commit")
	}
	result, status, err := a.updaterDispatch(r, u, "apply", "/apply", struct {
		Commit string `json:"commit"`
	}{input.Commit}, input)
	if err != nil {
		return err
	}
	jsonResponse(w, status, result)
	return nil
}
func (a *App) updaterRecover(w http.ResponseWriter, r *http.Request, u Principal) error {
	if err := updaterAdmin(u); err != nil {
		return err
	}
	var input struct {
		OperationID  string `json:"operation_id"`
		Confirmation string `json:"confirmation"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if err := decode(r, &input); err != nil {
		return err
	}
	if !updaterOperation.MatchString(input.OperationID) || input.Confirmation != input.OperationID {
		return bad("Confirm the exact recovery operation")
	}
	result, status, err := a.updaterDispatch(r, u, "recover", "/recover", struct {
		OperationID string `json:"operation_id"`
	}{input.OperationID}, input)
	if err != nil {
		return err
	}
	jsonResponse(w, status, result)
	return nil
}
