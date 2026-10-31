package planner

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"
)

var pwEngines = []string{"chromium", "firefox", "webkit"}
var pwRevisions = map[string]string{"chromium": "1248", "firefox": "1555", "webkit": "2370"}
var pwScenarios = []string{"B01", "B02", "B03", "B04", "B05", "B06", "B07", "B08", "B09"}

// Call logs may include credentials, so they remain private and bounded. The
// child can finish writing, but crossing the bound blocks acceptance.
type pwBoundedLog struct {
	mu        sync.Mutex
	writer    io.Writer
	remaining int
	truncated bool
}

func (log *pwBoundedLog) Write(value []byte) (int, error) {
	log.mu.Lock()
	defer log.mu.Unlock()
	keep := len(value)
	if keep > log.remaining {
		keep = log.remaining
		log.truncated = true
	}
	if keep > 0 {
		if _, err := log.writer.Write(value[:keep]); err != nil {
			return 0, err
		}
		log.remaining -= keep
	}
	return len(value), nil
}

type pwScenario struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Attempts int    `json:"attempts"`
}
type pwReceipt struct {
	FailurePhase  string       `json:"failure_phase,omitempty"`
	FailureSite   string       `json:"failure_site,omitempty"`
	FailureClass  string       `json:"failure_class,omitempty"`
	Format        string       `json:"format"`
	SchemaVersion int          `json:"schema_version"`
	Nonce         string       `json:"run_nonce"`
	Engine        string       `json:"engine"`
	Playwright    string       `json:"playwright_version"`
	Revision      string       `json:"browser_revision"`
	Browser       string       `json:"browser_version"`
	Status        string       `json:"status"`
	TestCount     int          `json:"test_count"`
	Scenarios     []pwScenario `json:"scenarios"`
	FunctionalTLS string       `json:"functional_tls"`
	StrictTLS     string       `json:"strict_tls"`
}

func validatePWReceipt(raw []byte, engine, nonce string) error {
	fail := errors.New("invalid browser receipt")
	if len(raw) == 0 || len(raw) > 262144 {
		return fail
	}
	var receipt pwReceipt
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if !uniquePWJSON(raw) || decoder.Decode(&receipt) != nil {
		return fail
	}
	var tail any
	if decoder.Decode(&tail) != io.EOF {
		return fail
	}
	if receipt.FailurePhase != "" || receipt.FailureSite != "" || receipt.FailureClass != "" {
		return fail
	}
	if receipt.Format != "gm-vector-browser-receipt" || receipt.SchemaVersion != 1 || receipt.Engine != engine || pwRevisions[engine] == "" || receipt.Nonce != nonce || receipt.Playwright != "1.64.1" || receipt.Revision != pwRevisions[engine] || !regexp.MustCompile(`^[0-9]+(\.[0-9]+)+$`).MatchString(receipt.Browser) || receipt.Status != "passed" || receipt.TestCount != 1 || len(receipt.Scenarios) != 9 {
		return fail
	}
	if receipt.FunctionalTLS != "owned-loopback-self-signed-exception" || receipt.StrictTLS != "unknown-ca-and-mismatch-rejected-not-isolated-hostname-proof" {
		return fail
	}
	for i, item := range receipt.Scenarios {
		if item.ID != pwScenarios[i] || item.Status != "passed" || item.Attempts != 1 {
			return fail
		}
	}
	return nil
}
func uniquePWJSON(raw []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var value func(int) bool
	value = func(depth int) bool {
		if depth > 32 {
			return false
		}
		item, err := decoder.Token()
		if err != nil {
			return false
		}
		delimiter, container := item.(json.Delim)
		if !container {
			return true
		}
		if delimiter == '{' {
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] {
					return false
				}
				seen[name] = true
				if !value(depth + 1) {
					return false
				}
			}
		} else if delimiter == '[' {
			for decoder.More() {
				if !value(depth + 1) {
					return false
				}
			}
		} else {
			return false
		}
		_, err = decoder.Token()
		return err == nil
	}
	if !value(0) {
		return false
	}
	_, err := decoder.Token()
	return err == io.EOF
}
func TestPlaywrightHarnessValidation(t *testing.T) {
	var bounded bytes.Buffer
	log := &pwBoundedLog{writer: &bounded, remaining: 4}
	if n, err := log.Write([]byte("abcdef")); n != 6 || err != nil || !log.truncated || bounded.String() != "abcd" {
		t.Fatal("private log bound failed")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node24 required for IPC contract unit")
	}
	if exec.Command(node, "../../web/browser/contracts.unit.cjs").Run() != nil {
		t.Fatal("private IPC/reporter unit failed")
	}

	good := pwReceipt{Format: "gm-vector-browser-receipt", SchemaVersion: 1, Nonce: "synthetic", Engine: "chromium", Playwright: "1.64.1", Revision: "1248", Browser: "156.0.8078.4", Status: "passed", TestCount: 1, FunctionalTLS: "owned-loopback-self-signed-exception", StrictTLS: "unknown-ca-and-mismatch-rejected-not-isolated-hostname-proof"}
	for _, id := range pwScenarios {
		good.Scenarios = append(good.Scenarios, pwScenario{id, "passed", 1})
	}
	encode := func(value pwReceipt) []byte { raw, _ := json.Marshal(value); return raw }
	if validatePWReceipt(encode(good), "chromium", "synthetic") != nil {
		t.Fatal("valid receipt rejected")
	}
	mutations := map[string]func(*pwReceipt){"wrong_nonce": func(v *pwReceipt) { v.Nonce = "other" }, "wrong_engine": func(v *pwReceipt) { v.Engine = "firefox" }, "wrong_revision": func(v *pwReceipt) { v.Revision = "old" }, "wrong_version": func(v *pwReceipt) { v.Playwright = "1.0" }, "zero_tests": func(v *pwReceipt) { v.TestCount = 0 }, "extra_tests": func(v *pwReceipt) { v.TestCount = 2 }, "failed": func(v *pwReceipt) { v.Status = "failed" }, "missing": func(v *pwReceipt) { v.Scenarios = v.Scenarios[:8] }, "duplicate": func(v *pwReceipt) { v.Scenarios[1].ID = "B01" }, "skipped": func(v *pwReceipt) { v.Scenarios[0].Status = "skipped" }, "retried": func(v *pwReceipt) { v.Scenarios[0].Attempts = 2 }, "empty_browser": func(v *pwReceipt) { v.Browser = "" }, "tls_claim": func(v *pwReceipt) { v.FunctionalTLS = "trusted" }}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			v := good
			v.Scenarios = append([]pwScenario{}, good.Scenarios...)
			mutate(&v)
			if validatePWReceipt(encode(v), "chromium", "synthetic") == nil {
				t.Fatal("invalid receipt admitted")
			}
		})
	}
	for _, raw := range [][]byte{nil, []byte("{}"), append(encode(good), []byte(" {}")...), bytes.Repeat([]byte("x"), 262145), bytes.Replace(encode(good), []byte(`"schema_version":1`), []byte(`"unexpected":1,"schema_version":1`), 1), bytes.Replace(encode(good), []byte(`"schema_version":1`), []byte(`"schema_version":2,"schema_version":1`), 1)} {
		if validatePWReceipt(raw, "chromium", "synthetic") == nil {
			t.Fatal("malformed receipt admitted")
		}
	}
}

// The three engines always run; a manually filtered Go child cannot become a
// passing full root. This fixture never attaches to an installation or browser.
func TestPlaywrightE2E(t *testing.T) {
	if os.Getenv("GMA_PLAYWRIGHT_E2E") != "1" {
		t.Skip("set GMA_PLAYWRIGHT_E2E=1 for isolated three-engine E2E")
	}
	dsn, err := url.Parse(os.Getenv("GMA_TEST_DSN"))
	if err != nil || dsn.Path != "/gma_planner_test" || (dsn.Scheme != "postgres" && dsn.Scheme != "postgresql") {
		t.Fatal("explicit disposable database required")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node24 required")
	}
	web, err := filepath.Abs("../../web")
	if err != nil {
		t.Fatal("web fixture unavailable")
	}
	for _, file := range []string{"dist/index.html", "node_modules/@playwright/test/cli.js", "node_modules/playwright-core/browsers.json"} {
		if _, e := os.Stat(filepath.Join(web, file)); e != nil {
			t.Fatal("build and pinned Playwright prerequisites required")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 570*time.Second)
	defer cancel()
	completed := 0
	for _, engine := range pwEngines {
		t.Run(engine, func(t *testing.T) { completed++; runPWEngine(t, ctx, node, web, dsn.String(), engine) })
	}
	if completed != 3 {
		t.Fatal("all three required engines must execute")
	}
}
func runPWEngine(t *testing.T, parent context.Context, node, web, dsn, engine string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 180*time.Second)
	defer cancel()
	root, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal("test database unavailable")
	}
	defer root.DB.Close()
	schema := "pw_" + newID()
	if _, err = root.DB.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal("isolated schema unavailable")
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if _, e := root.DB.Exec(cleanup, "DROP SCHEMA "+schema+" CASCADE"); e != nil {
			t.Error("schema cleanup failed")
		}
	}()
	parsed, _ := url.Parse(dsn)
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	store, err := Open(ctx, parsed.String())
	if err != nil {
		t.Fatal("isolated store unavailable")
	}
	defer store.DB.Close()
	if store.Migrate(ctx) != nil {
		t.Fatal("fixture migration failed")
	}
	server := httptest.NewUnstartedServer(nil)
	defer server.Close()
	runDir := t.TempDir()
	if os.Chmod(runDir, 0700) != nil {
		t.Fatal("private directory unavailable")
	}
	support, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		t.Fatal("synthetic support key unavailable")
	}
	modulus := base64.RawURLEncoding.EncodeToString(support.N.Bytes())
	thumb := sha256.Sum256([]byte(`{"e":"AQAB","kty":"RSA","n":"` + modulus + `"}`))
	cfg := Config{Origin: "https://" + server.Listener.Addr().String(), WebDir: filepath.Join(web, "dist"), Secret: random(32), PlanningTimezone: "America/New_York", ReportDirectory: filepath.Join(runDir, "reports"), ReportPublicKey: &ReportPublicKey{Kty: "RSA", N: modulus, E: "AQAB", Alg: "RSA-OAEP-256", Use: "enc", Kid: base64.RawURLEncoding.EncodeToString(thumb[:])}}
	if os.Mkdir(cfg.ReportDirectory, 0700) != nil {
		t.Fatal("private report fixture unavailable")
	}
	app, err := NewApp(store, cfg)
	if err != nil {
		t.Fatal("app fixture unavailable")
	}
	nonce := token()
	handler := app.Handler()
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-GM-Playwright-Run", nonce)
		handler.ServeHTTP(w, r)
	})
	server.StartTLS()
	_, calendar, _ := fixture()
	if app.SeedCalendar(ctx, calendar) != nil {
		t.Fatal("calendar fixture unavailable")
	}
	// Retained history exceeds the legacy project limit and includes root-owned
	// and NULL-employee facts; it deliberately predates the planning year.
	if _, err = store.DB.Exec(ctx, "INSERT INTO projects(id,title,start_date,duration_mode,calendar_id) VALUES('pw-history','PW retained history','2026-01-05','working',$1)", calendar.ID); err != nil {
		t.Fatal("history project unavailable")
	}
	if _, err = store.DB.Exec(ctx, ` INSERT INTO tasks(project_id,id,title,status,legacy_actual_minutes) VALUES('pw-history','history-root','History root','todo',23);
 INSERT INTO tasks(project_id,id,parent_id,title,status,legacy_actual_minutes) VALUES('pw-history','history-child','history-root','History child','todo',17);
 INSERT INTO employees(id,name,daily_minutes) VALUES('history-employee','Synthetic historical employee',480);
 INSERT INTO time_entries(id,project_id,task_id,employee_id,work_date,minutes,comment) SELECT 'pw-entry-'||lpad(n::text,4,'0'),'pw-history',CASE WHEN n=1 THEN 'history-root' ELSE 'history-child' END,CASE WHEN n=1 THEN NULL ELSE 'history-employee' END,'2024-02-29',1,'Synthetic retained fact' FROM generate_series(1,1002) AS n;`); err != nil {
		t.Fatal("history fixture unavailable")
	}

	key, _ := x509.MarshalPKCS8PrivateKey(support)
	input := map[string]any{"format": "gm-vector-playwright-fixture", "schema_version": 1, "engine": engine, "nonce": nonce, "origin": cfg.Origin, "certificate": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})), "username": "pw-admin", "password": token() + "-synthetic", "report_private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})), "calendar_id": calendar.ID, "history_project": "pw-history"}
	raw, _ := json.Marshal(input)
	file := filepath.Join(runDir, "fixture.json")
	ipc, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("private IPC unavailable")
	}
	if _, err = ipc.Write(raw); err != nil {
		ipc.Close()
		t.Fatal("private IPC write failed")
	}
	if ipc.Close() != nil {
		t.Fatal("private IPC close failed")
	}
	// Child output is private; Playwright call logs can contain input values.
	private, err := os.OpenFile(filepath.Join(runDir, "process.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("private child log unavailable")
	}
	defer private.Close()
	cmd := exec.CommandContext(ctx, node, filepath.Join(web, "node_modules/@playwright/test/cli.js"), "test", "--config=playwright.config.ts", "--project="+engine)
	cmd.Dir = web
	for _, name := range []string{"PATH", "HOME", "LANG", "LC_ALL", "TZ", "XDG_CACHE_HOME", "PLAYWRIGHT_BROWSERS_PATH", "LD_LIBRARY_PATH", "GIO_EXTRA_MODULES", "GST_PLUGIN_PATH", "GST_PLUGIN_SYSTEM_PATH", "FONTCONFIG_PATH", "XDG_DATA_DIRS"} {
		if value, ok := os.LookupEnv(name); ok {
			cmd.Env = append(cmd.Env, name+"="+value)
		}
	}
	cmd.Env = append(cmd.Env, "GMA_PW_FIXTURE_FILE="+file)
	log := &pwBoundedLog{writer: private, remaining: 1048576}
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 5 * time.Second
	runErr := cmd.Run()
	if log.truncated {
		t.Fatal("private browser diagnostics exceeded 1MiB bound")
	}
	receiptFile := filepath.Join(runDir, "receipt.json")
	info, statErr := os.Lstat(receiptFile)
	if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 262144 {
		t.Fatal("required private engine receipt unavailable")
	}
	receipt, readErr := os.ReadFile(receiptFile)
	if readErr != nil || validatePWReceipt(receipt, engine, nonce) != nil || runErr != nil {
		var partial pwReceipt
		_ = json.Unmarshal(receipt, &partial)
		phase := "startup"
		for i, id := range pwScenarios {
			if i >= len(partial.Scenarios) || partial.Scenarios[i].ID != id {
				phase = id
				break
			}
		}
		site := "unknown"
		if regexp.MustCompile(`^(critical\.spec|fixtures|contracts)\.ts:[0-9]+:[0-9]+$`).MatchString(partial.FailureSite) {
			site = partial.FailureSite
		}
		t.Fatalf("three-engine scenario failed; engine=%s phase=%s site=%s class=Error; private output not published", engine, phase, site)
	}
	t.Log("Playwright engine=" + engine + " version=1.64.1 scenarios=B01-B09 passed=9 skipped=0 retries=0 functional_tls=synthetic-only")
}
