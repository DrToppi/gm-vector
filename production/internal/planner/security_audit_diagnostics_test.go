package planner

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSecurityAuditDiagnosticMarkerCorpus(t *testing.T) {
	a := &App{Config: Config{Origin: "https://audit.invalid", WebDir: t.TempDir()}}
	marker := "SYNTHETIC-NONEXPORT-MARKER"
	body := `{"title":"` + marker + `","comment":"` + marker + `","email":"` + marker + `@example.invalid","password":"` + marker + `"}`
	r := httptest.NewRequest("POST", "https://audit.invalid/api/v1/auth/login?token="+marker+"#"+marker, strings.NewReader(body))
	for _, header := range []string{"Authorization", "Cookie", "X-Request-ID", "Referer", "X-Forwarded-For"} {
		r.Header.Set(header, marker)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	events := a.diagnosticEvents.snapshot(time.Now(), "", true)
	raw, _ := json.Marshal(events)
	if len(events) != 1 || strings.Contains(string(raw), marker) {
		t.Fatal("marker leaked or event missing")
	}
}
func TestSecurityAuditPrivateJWKFields(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		t.Fatal(err)
	}
	pub := ReportPublicKey{Kty: "RSA", N: base64.RawURLEncoding.EncodeToString(key.N.Bytes()), E: "AQAB", Alg: "RSA-OAEP-256", Use: "enc"}
	pub.Kid = reportKeyID(pub)
	raw, _ := json.Marshal(pub)
	file := filepath.Join(t.TempDir(), "key.json")
	for _, field := range []string{"d", "p", "q", "dp", "dq", "qi", "oth", "password", "private_key"} {
		bad := strings.TrimSuffix(string(raw), "}") + `,"` + field + `":"SYNTHETIC-PRIVATE-PARAMETER"}`
		if err := os.WriteFile(file, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := loadReportPublicKey(file)
		if err == nil || strings.Contains(err.Error(), "SYNTHETIC") {
			t.Fatal("private JWK parameter accepted or disclosed", field)
		}
	}
	a := &App{Config: Config{ReportPublicKey: &pub}}
	w := httptest.NewRecorder()
	if err := a.reportKey(w, httptest.NewRequest("GET", "/", nil), Principal{}); err != nil {
		t.Fatal(err)
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(w.Body.Bytes(), &envelope) != nil || len(envelope) != 2 {
		t.Fatal("unexpected envelope")
	}
	var fields map[string]any
	if json.Unmarshal(envelope["key"], &fields) != nil || len(fields) != 6 {
		t.Fatal("unexpected public key fields")
	}
	for _, field := range []string{"kty", "n", "e", "alg", "use", "kid"} {
		if _, ok := fields[field]; !ok {
			t.Fatal("missing field", field)
		}
	}
}
