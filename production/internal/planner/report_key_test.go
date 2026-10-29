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
)

func TestReportPublicKey(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		t.Fatal(err)
	}
	k := ReportPublicKey{Kty: "RSA", N: base64.RawURLEncoding.EncodeToString(private.N.Bytes()), E: "AQAB", Alg: "RSA-OAEP-256", Use: "enc"}
	k.Kid = reportKeyID(k)
	if err = validateReportPublicKey(k); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(k)
	path := filepath.Join(t.TempDir(), "public.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadReportPublicKey(path)
	if err != nil || *loaded != k {
		t.Fatalf("load failed: %v", err)
	}
	a := &App{Config: Config{ReportPublicKey: loaded}}
	response := httptest.NewRecorder()
	if err = a.reportKey(response, httptest.NewRequest("GET", "/api/v1/diagnostics/key", nil), Principal{}); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Configured bool            `json:"configured"`
		Key        ReportPublicKey `json:"key"`
	}
	if json.Unmarshal(response.Body.Bytes(), &got) != nil || !got.Configured || got.Key != k {
		t.Fatal("public response differs")
	}
	for _, mutate := range []func(*ReportPublicKey){
		func(k *ReportPublicKey) { k.Kid = "email@example.invalid" }, func(k *ReportPublicKey) { k.E = "Aw" }, func(k *ReportPublicKey) { k.N = "AA" }, func(k *ReportPublicKey) { k.Alg = "RSA1_5" }, func(k *ReportPublicKey) { k.Use = "sig" },
	} {
		bad := k
		mutate(&bad)
		if validateReportPublicKey(bad) == nil {
			t.Fatal("invalid key accepted")
		}
	}
	for _, bad := range []string{strings.TrimSuffix(string(raw), "}") + `,"d":"private-marker"}`, string(raw) + ` {}`, strings.Repeat("x", 16385)} {
		if err = os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = loadReportPublicKey(path); err == nil || strings.Contains(err.Error(), "private-marker") {
			t.Fatal("invalid key accepted or data leaked")
		}
	}
	response = httptest.NewRecorder()
	a.Config.ReportPublicKey = nil
	if err = a.reportKey(response, httptest.NewRequest("GET", "/", nil), Principal{}); err != nil || strings.TrimSpace(response.Body.String()) != `{"configured":false}` {
		t.Fatal("missing-key response")
	}
}
