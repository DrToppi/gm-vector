package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type RuntimeConfig struct {
	ReportDirectory     string   `json:"report_directory,omitempty"`
	TrustedProxyCIDRs   []string `json:"trusted_proxy_cidrs,omitempty"`
	DatabaseURL         string   `json:"database_url"`
	Origin              string   `json:"origin"`
	WebDir              string   `json:"web_dir"`
	SecretFile          string   `json:"secret_file"`
	BootstrapFile       string   `json:"bootstrap_file"`
	LDAPFile            string   `json:"ldap_file,omitempty"`
	SMTPFile            string   `json:"smtp_file,omitempty"`
	ReportPublicKeyFile string   `json:"report_public_key_file,omitempty"`
}

func LoadRuntime(path string) (RuntimeConfig, Config, error) {
	var r RuntimeConfig
	var c Config
	b, e := os.ReadFile(path)
	if e != nil {
		return r, c, e
	}
	if e = json.Unmarshal(b, &r); e != nil {
		return r, c, e
	}
	key, e := os.ReadFile(r.SecretFile)
	if e != nil {
		return r, c, e
	}
	bootstrap, e := os.ReadFile(r.BootstrapFile)
	if e != nil {
		return r, c, e
	}
	_, release, e := RuntimeReleaseTrust()
	if e != nil {
		return r, c, e
	}
	Version = release.Version
	ReleaseDate, e = time.Parse("2006-01-02", release.ReleaseDate)
	if e != nil {
		return r, c, e
	}
	c = Config{ReportDirectory: r.ReportDirectory, Origin: r.Origin, WebDir: r.WebDir, Secret: key, Bootstrap: string(bootstrap)}
	proxyCIDRs := r.TrustedProxyCIDRs
	if override := os.Getenv("GMA_TRUSTED_PROXY_CIDRS"); override != "" {
		proxyCIDRs = strings.Split(override, ",")
	}
	c.TrustedProxies, e = parseTrustedProxies(proxyCIDRs)
	if e != nil {
		return r, c, e
	}
	if r.LDAPFile != "" {
		c.LDAP, e = loadLDAPConfig(r.LDAPFile)
		if e != nil {
			return r, c, e
		}
	}
	if r.SMTPFile != "" {
		c.SMTP, e = loadSMTP(r.SMTPFile)
		if e != nil {
			return r, c, e
		}
	}
	if r.ReportPublicKeyFile != "" {
		c.ReportPublicKey, e = loadReportPublicKey(r.ReportPublicKeyFile)
		if e != nil {
			return r, c, e
		}
	}
	return r, c, nil
}
func writeNew(path string, data []byte) error {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	if _, e = f.Write(data); e != nil {
		return e
	}
	return f.Sync()
}

// Initialize creates only installation secrets; no publisher or entitlement is needed.
func Initialize(dir, origin string) error {
	u, e := url.Parse(origin)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("origin must be an HTTPS origin without credentials or path")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	if _, e := os.Stat(filepath.Join(dir, "config.json")); e == nil {
		return errors.New("already initialized; no secrets overwritten")
	}
	password, runtimePassword, migrationPassword, bootstrap := token(), token(), token(), token()
	for name, b := range map[string][]byte{"instance.id": []byte(newID()), "encryption.key": random(32), "bootstrap.token": []byte(bootstrap), "database.password": []byte(password), "setup-url.txt": []byte(origin + "/#setup=" + bootstrap)} {
		if e := writeNew(filepath.Join(dir, name), b); e != nil {
			return e
		}
	}
	r := RuntimeConfig{DatabaseURL: "postgres://planner_runtime:" + runtimePassword + "@db:5432/planner?sslmode=disable", Origin: origin, WebDir: "/app/web", SecretFile: "/run/secrets/encryption.key", BootstrapFile: "/run/secrets/bootstrap.token"}
	migration := MigrationConfig{DatabaseURL: "postgres://planner_migrator:" + migrationPassword + "@db:5432/planner?sslmode=disable", RuntimeRole: "planner_runtime"}
	raw, _ := json.MarshalIndent(migration, "", "  ")
	if e := writeNew(filepath.Join(dir, "migration-config.json"), raw); e != nil {
		return e
	}
	raw, _ = json.MarshalIndent(r, "", "  ")
	return writeNew(filepath.Join(dir, "config.json"), raw)
}
func (a *App) Worker(ctx context.Context) error {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if e := a.workerTick(ctx); e != nil {
			slog.Error("worker tick failed")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
func (a *App) workerTick(ctx context.Context) error {
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	// External integrations are independently opt-in.
	if _, e = tx.Exec(ctx, "INSERT INTO settings(key,value) VALUES('worker_heartbeat',to_jsonb(now())) ON CONFLICT(key) DO UPDATE SET value=excluded.value"); e != nil {
		return e
	}
	for _, sql := range []string{"DELETE FROM sessions WHERE created_at<now()-interval '12 hours' OR seen_at<now()-interval '30 minutes'", "DELETE FROM enrollments WHERE expires_at<now()", "DELETE FROM login_limits WHERE window_start<now()-interval '1 day'", "DELETE FROM audit WHERE created_at<now()-interval '365 days'", "DELETE FROM snapshots WHERE created_at<now()-interval '365 days'", "DELETE FROM projects WHERE archived_at<now()-interval '30 days'"} {
		if _, e = tx.Exec(ctx, sql); e != nil {
			return e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	if e = a.customerMailTick(ctx); e != nil {
		return e
	}
	return a.invitationMailTick(ctx)
}
func CheckFileMode(path string) error {
	st, e := os.Stat(path)
	if e != nil {
		return e
	}
	if st.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("secret file requires mode 0600: %s", filepath.Base(path))
	}
	return nil
}
