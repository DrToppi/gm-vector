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
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestBrowserE2E never attaches to a running application or browser. Opting in
// requires an explicit test database; missing prerequisites are failures.
func TestBrowserE2E(t *testing.T) {
	if os.Getenv("GMA_E2E") != "1" {
		t.Skip("set GMA_E2E=1 for isolated browser E2E")
	}
	dsn, err := url.Parse(os.Getenv("GMA_TEST_DSN"))
	if err != nil || dsn.Path != "/gma_planner_test" || (dsn.Scheme != "postgres" && dsn.Scheme != "postgresql") {
		t.Fatal("GMA_TEST_DSN must explicitly select gma_planner_test")
	}
	chrome := os.Getenv("GMA_CHROME_BIN")
	if chrome == "" {
		for _, name := range []string{"google-chrome", "chromium", "chromium-browser"} {
			if p, e := exec.LookPath(name); e == nil {
				chrome = p
				break
			}
		}
	}
	if chrome == "" {
		t.Fatal("Chrome/Chromium required; set GMA_CHROME_BIN")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node 24 required")
	}
	web, err := filepath.Abs("../../web/dist")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(web, "index.html")); err != nil {
		t.Fatal("build production/web before E2E")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	root, err := Open(ctx, dsn.String())
	if err != nil {
		t.Fatal("test PostgreSQL unavailable")
	}
	defer root.DB.Close()
	schema := "e2e_" + newID()
	if _, err = root.DB.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal("cannot create isolated test schema")
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if _, e := root.DB.Exec(cleanup, "DROP SCHEMA "+schema+" CASCADE"); e != nil {
			t.Error("cannot remove E2E schema")
		}
	}()
	query := dsn.Query()
	query.Set("search_path", schema)
	dsn.RawQuery = query.Encode()
	store, err := Open(ctx, dsn.String())
	if err != nil {
		t.Fatal("cannot open isolated schema")
	}
	defer store.DB.Close()
	if err = store.Migrate(ctx); err != nil {
		t.Fatal("E2E migration failed")
	}
	server := httptest.NewUnstartedServer(nil)
	defer server.Close()
	cfg := Config{Origin: "https://" + server.Listener.Addr().String(), WebDir: web, Secret: random(32)}
	// Ephemeral synthetic support key; never loaded from a production installation.
	reportPrivate, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		t.Fatal(err)
	}
	modulus := base64.RawURLEncoding.EncodeToString(reportPrivate.N.Bytes())
	thumbprint := sha256.Sum256([]byte(`{"e":"AQAB","kty":"RSA","n":"` + modulus + `"}`))
	cfg.ReportDirectory = t.TempDir()
	if err := os.Chmod(cfg.ReportDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	cfg.ReportPublicKey = &ReportPublicKey{Kty: "RSA", N: modulus, E: "AQAB", Alg: "RSA-OAEP-256", Use: "enc", Kid: base64.RawURLEncoding.EncodeToString(thumbprint[:])}
	reportPKCS8, err := x509.MarshalPKCS8PrivateKey(reportPrivate)
	if err != nil {
		t.Fatal(err)
	}
	reportPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: reportPKCS8}))
	app, err := NewApp(store, cfg)
	if err != nil {
		t.Fatal("E2E app setup failed")
	}
	server.Config.Handler = app.Handler()
	server.StartTLS()
	_, cal, _ := fixture()
	if err = app.SeedCalendar(ctx, cal); err != nil {
		t.Fatal("fixture calendar failed")
	}
	password := token() + "-e2e"
	// Synthetic credentials travel over stdin, never process arguments or artifacts.
	input, err := json.Marshal(map[string]string{"origin": cfg.Origin, "username": "e2e-admin", "password": password, "chrome": chrome, "report_private_key": reportPEM, "report_directory": cfg.ReportDirectory, "profile": t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, node, "../../scripts/e2e-check.cjs")
	cmd.Stdin = bytes.NewReader(input)
	// Let Node close its owned Chrome before the forced WaitDelay timeout.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 5 * time.Second
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser E2E failed: %s", strings.TrimSpace(string(output)))
	}
	t.Log(strings.TrimSpace(string(output)))
}
