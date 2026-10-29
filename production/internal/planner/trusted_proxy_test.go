package planner

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTrustedProxyAddress(t *testing.T) {
	networks, e := parseTrustedProxies([]string{"192.0.2.1/32", "2001:db8::1/128"})
	if e != nil {
		t.Fatal(e)
	}
	a := &App{Config: Config{TrustedProxies: networks}}
	cases := []struct {
		peer, forward, want string
		bad                 bool
	}{
		{"198.51.100.2:42", "203.0.113.9", "198.51.100.2", false},
		{"192.0.2.1:42", "203.0.113.9", "203.0.113.9", false},
		{"[::ffff:192.0.2.1]:42", "::ffff:203.0.113.9", "203.0.113.9", false},
		{"[2001:db8::1]:42", "2001:db8::2", "2001:db8::2", false},
		{"192.0.2.1:42", "203.0.113.9, 203.0.113.10", "", true},
		{"192.0.2.1:42", "", "", true}, {"192.0.2.1:42", "fe80::1%eth0", "", true},
	}
	for _, c := range cases {
		r := httptest.NewRequest("POST", "/", nil)
		r.RemoteAddr = c.peer
		r.Header.Set("X-GMA-Client-IP", c.forward)
		r.Header.Set("X-Forwarded-For", "sensitive-forged")
		got, e := a.loginClientIP(r)
		if (e != nil) != c.bad || got != c.want {
			t.Fatalf("unexpected address policy result for %s", c.peer)
		}
	}
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "192.0.2.1:42"
	r.Header.Add("X-GMA-Client-IP", "203.0.113.1")
	r.Header.Add("X-GMA-Client-IP", "203.0.113.2")
	if _, e = a.loginClientIP(r); e == nil {
		t.Fatal("multiple header values accepted")
	}
	for _, v := range []string{"0.0.0.0/0", "::/0", "invalid", "::ffff:127.0.0.1/128"} {
		if _, e = parseTrustedProxies([]string{v}); e == nil {
			t.Fatal("unsafe CIDR accepted")
		}
	}
}

func TestTrustedProxyIntegration(t *testing.T) {
	dsn := os.Getenv("GMA_TEST_DSN")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL")
	}
	u, e := url.Parse(dsn)
	if e != nil || u.Path != "/gma_planner_test" {
		t.Fatal("requires isolated test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	root, e := Open(ctx, dsn)
	if e != nil {
		t.Fatal("test DB unavailable")
	}
	defer root.DB.Close()
	schema := "proxy_" + newID()
	if _, e = root.DB.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer root.DB.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	s, e := Open(ctx, u.String())
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	trusted, _ := parseTrustedProxies([]string{"127.0.0.1/32"})
	a := &App{Store: s, Config: Config{TrustedProxies: trusted}}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := a.throttle(r, r.URL.Query().Get("account")); e != nil {
			if ae, ok := e.(appError); ok {
				w.WriteHeader(ae.Status)
			} else {
				w.WriteHeader(500)
			}
			return
		}
		w.WriteHeader(200)
	}))
	defer backend.Close()
	binary, e := filepath.Abs("../../bin/caddy")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(binary); e != nil {
		t.Fatal("build Caddy before required proxy integration test")
	}
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	address := listener.Addr().String()
	listener.Close()
	// Reuse the exact production header directive, so the test detects regressions there.
	source, e := os.ReadFile("../../Caddyfile")
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(source), "header_up X-GMA-Client-IP {http.request.remote.host}") {
		t.Fatal("production Caddy must overwrite client address")
	}
	config := fmt.Sprintf("{\n admin off\n}\nhttp://%s {\n reverse_proxy %s {\n header_up X-GMA-Client-IP {http.request.remote.host}\n }\n}\n", address, strings.TrimPrefix(backend.URL, "http://"))
	dir := t.TempDir()
	path := filepath.Join(dir, "Caddyfile")
	if e = os.WriteFile(path, []byte(config), 0600); e != nil {
		t.Fatal(e)
	}
	cmd := exec.CommandContext(ctx, binary, "run", "--config", path, "--adapter", "caddyfile")
	cmd.Env = append(os.Environ(), "HOME="+dir, "XDG_DATA_HOME="+dir)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if e = cmd.Start(); e != nil {
		t.Fatal("test Caddy failed to start")
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	for i := 0; i < 100; i++ {
		conn, err := net.DialTimeout("tcp", address, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		if i == 99 {
			t.Fatal("Caddy startup timeout")
		}
		time.Sleep(25 * time.Millisecond)
	}
	client := func(ip string) *http.Client {
		d := &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(ip)}}
		tr := &http.Transport{DialContext: d.DialContext}
		t.Cleanup(tr.CloseIdleConnections)
		return &http.Client{Transport: tr, Timeout: 3 * time.Second}
	}
	first, second := client("127.0.0.2"), client("127.0.0.3")
	request := func(c *http.Client, account string) int {
		r, _ := http.NewRequest("POST", "http://"+address+"/?account="+account, nil)
		r.Header.Set("X-GMA-Client-IP", "203.0.113.99")
		r.Header.Set("X-Forwarded-For", "203.0.113.98")
		res, e := c.Do(r)
		if e != nil {
			t.Fatal("proxy test request failed")
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		return res.StatusCode
	}
	for i := 0; i < 100; i++ {
		if code := request(first, fmt.Sprintf("first-%d", i)); code != 200 {
			t.Fatalf("first client attempt%d: %d", i, code)
		}
	}
	if request(first, "over-limit") != 429 {
		t.Fatal("client limit bypassed by spoofed headers")
	}
	if request(second, "independent") != 200 {
		t.Fatal("first client blocked second through Caddy")
	}
	for i := 0; i < 10; i++ {
		if request(second, "account-limit") != 200 {
			t.Fatal("account early denial")
		}
	}
	if request(second, "account-limit") != 429 {
		t.Fatal("account budget bypassed")
	}
}
