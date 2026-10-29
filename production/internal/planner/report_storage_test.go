package planner

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func storageTestKey() ReportPublicKey {
	m := make([]byte, 384)
	m[0] = 128
	m[383] = 1
	k := ReportPublicKey{Kty: "RSA", N: base64.RawURLEncoding.EncodeToString(m), E: "AQAB", Alg: "RSA-OAEP-256", Use: "enc"}
	k.Kid = reportKeyID(k)
	return k
}
func storageEnvelope(k ReportPublicKey, s string) string {
	h, _ := json.Marshal(map[string]string{"alg": "RSA-OAEP-256", "enc": "A256GCM", "kid": k.Kid, "typ": "gma-bug-report+jwe", "cty": "application/json"})
	e := base64.RawURLEncoding.EncodeToString
	return strings.Join([]string{e(h), e(make([]byte, 384)), e(make([]byte, 12)), e([]byte("cipher" + s)), e(make([]byte, 16))}, ".")
}
func TestReportEnvelopeValidation(t *testing.T) {
	k := storageTestKey()
	good := storageEnvelope(k, "")
	if !validateReportEnvelope(good, k) {
		t.Fatal("valid envelope rejected")
	}
	parts := strings.Split(good, ".")
	cases := []string{"", good + ".", strings.Replace(good, ".", "=.", 1), strings.Repeat("a", reportMaxBytes+1)}
	for _, extra := range []string{`,"jku":"https://invalid"`, `,"alg":"RSA-OAEP-256"`} {
		h, _ := base64.RawURLEncoding.DecodeString(parts[0])
		cases = append(cases, base64.RawURLEncoding.EncodeToString([]byte(strings.TrimSuffix(string(h), "}")+extra+"}"))+"."+strings.Join(parts[1:], "."))
	}
	for i := 1; i < 5; i++ {
		bad := append([]string(nil), parts...)
		bad[i] = ""
		cases = append(cases, strings.Join(bad, "."))
	}
	for i, c := range cases {
		if validateReportEnvelope(c, k) {
			t.Fatalf("invalid %d accepted", i)
		}
	}
	k.Kid = "wrong"
	if validateReportEnvelope(good, k) {
		t.Fatal("wrong key accepted")
	}
}
func storageDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if e := os.Chmod(d, 0700); e != nil {
		t.Fatal(e)
	}
	return d
}
func TestReportStoragePersistent(t *testing.T) {
	d := storageDir(t)
	s := storageEnvelope(storageTestKey(), "")
	id, created, e := writeReport(d, s, time.Now())
	if e != nil || !created {
		t.Fatalf("store: %v", e)
	}
	raw, e := os.ReadFile(filepath.Join(d, id+".gma-bug"))
	if e != nil || string(raw) != s {
		t.Fatal("payload differs")
	}
	info, _ := os.Stat(filepath.Join(d, id+".gma-bug"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("mode")
	}
	id2, created, e := writeReport(d, s, time.Now())
	if e != nil || created || id2 != id {
		t.Fatalf("retry: %v", e)
	}
	os.Chmod(d, 0755)
	if _, _, e = writeReport(d, s, time.Now()); e == nil {
		t.Fatal("insecure dir accepted")
	}
}
func TestReportStorageLimits(t *testing.T) {
	d := storageDir(t)
	k := storageTestKey()
	now := time.Now()
	for i := 0; i < reportPerMinute; i++ {
		if _, _, e := writeReport(d, storageEnvelope(k, fmt.Sprint(i)), now); e != nil {
			t.Fatal(e)
		}
	}
	_, _, e := writeReport(d, storageEnvelope(k, "over"), now)
	if a, ok := e.(appError); !ok || a.Status != 429 {
		t.Fatalf("rate: %v", e)
	}
	for i := reportPerMinute; i < reportMaxCount; i++ {
		if e = os.WriteFile(filepath.Join(d, fmt.Sprint(i)), []byte("fixture"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	_, _, e = writeReport(d, storageEnvelope(k, "over"), now.Add(time.Hour))
	if a, ok := e.(appError); !ok || a.Status != 507 {
		t.Fatalf("quota: %v", e)
	}
	other := storageDir(t)
	os.Symlink(d, filepath.Join(other, "link"))
	if _, _, e = writeReport(filepath.Join(other, "link"), "x", now); e == nil {
		t.Fatal("symlink dir accepted")
	}
	os.Symlink(filepath.Join(d, "outside"), filepath.Join(other, ".lock"))
	if _, _, e = writeReport(other, "x", now); e == nil {
		t.Fatal("symlink lock accepted")
	}
}
func TestReportStorageConcurrent(t *testing.T) {
	d := storageDir(t)
	k := storageTestKey()
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, e := writeReport(d, storageEnvelope(k, fmt.Sprint(i)), time.Now())
			if e != nil {
				if a, ok := e.(appError); !ok || a.Status != 429 {
					t.Errorf("unexpected: %v", e)
				}
			}
		}(i)
	}
	wg.Wait()
	entries, e := os.ReadDir(d)
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) > reportPerMinute+1 {
		t.Fatal("limit exceeded")
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".pending") {
			t.Fatal("partial file")
		}
	}
}
func TestBugReportHTTPGuards(t *testing.T) {
	d := storageDir(t)
	k := storageTestKey()
	a := &App{Config: Config{Origin: "https://example.invalid", ReportDirectory: d, ReportPublicKey: &k}}
	h := a.Handler()
	body, _ := json.Marshal(map[string]string{"encrypted": storageEnvelope(k, "")})
	for _, tc := range []struct {
		origin, body string
		want         int
	}{{"https://evil.invalid", string(body), 403}, {"https://example.invalid", `{"encrypted":"plaintext-secret"}`, 422}, {"https://example.invalid", string(body), 201}, {"https://example.invalid", string(body), 200}, {"https://example.invalid", `{"encrypted":"` + strings.Repeat("a", reportMaxBytes+1) + `"}`, 413}} {
		r := httptest.NewRequest("POST", "https://example.invalid/api/v1/diagnostics/reports", strings.NewReader(tc.body))
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("want%d got%d: %s", tc.want, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "plaintext-secret") {
			t.Fatal("leak")
		}
	}
	r := httptest.NewRequest("GET", "https://example.invalid/api/v1/diagnostics/reports", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code == 200 {
		t.Fatal("public listing")
	}
}
