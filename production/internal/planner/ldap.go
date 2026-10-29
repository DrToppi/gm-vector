package planner

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
)

type LDAPConfig struct {
	Enabled           bool   `json:"enabled"`
	URL               string `json:"url"`
	StartTLS          bool   `json:"start_tls"`
	Provider          string `json:"provider"`
	BindDN            string `json:"bind_dn"`
	BindPassword      string `json:"bind_password"`
	BaseDN            string `json:"base_dn"`
	Filter            string `json:"filter"`
	IDAttribute       string `json:"id_attribute"`
	BinaryID          bool   `json:"binary_id"`
	NameAttribute     string `json:"name_attribute"`
	EmailAttribute    string `json:"email_attribute"`
	UsernameAttribute string `json:"username_attribute"`
	ActiveAttribute   string `json:"active_attribute"`
	ActiveValue       string `json:"active_value"`
	DisabledBit       uint64 `json:"disabled_bit"`
	GroupsAttribute   string `json:"groups_attribute"`
	CAFile            string `json:"ca_file"`
	MaxEntries        int    `json:"max_entries"`
}

var ldapAttribute = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]*$`)

func loadLDAPConfig(path string) (*LDAPConfig, error) {
	if path == "" {
		return nil, nil
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		return nil, errors.New("Cannot read LDAP configuration")
	}
	if len(raw) > 65536 {
		return nil, errors.New("LDAP configuration too large")
	}
	var c LDAPConfig
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e = d.Decode(&c); e != nil {
		return nil, errors.New("Invalid LDAP configuration")
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, errors.New("Invalid LDAP configuration")
	}
	if e = c.validate(); e != nil {
		return nil, e
	}
	return &c, nil
}
func (c *LDAPConfig) validate() error {
	if !c.Enabled {
		return nil
	}
	u, e := url.Parse(c.URL)
	if e != nil || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || !(u.Scheme == "ldaps" || u.Scheme == "ldap" && c.StartTLS) {
		return errors.New("LDAP requires LDAPS or mandatory StartTLS")
	}
	if !validID.MatchString(c.Provider) || c.BaseDN == "" || c.BindDN == "" || c.BindPassword == "" {
		return errors.New("LDAP provider, base and service credentials are required")
	}
	if c.Filter == "" {
		c.Filter = "(objectClass=person)"
	}
	if _, e = ldap.CompileFilter(c.Filter); e != nil {
		return errors.New("Invalid LDAP search filter")
	}
	for _, s := range []string{c.IDAttribute, c.NameAttribute, c.EmailAttribute, c.UsernameAttribute, c.ActiveAttribute} {
		if !ldapAttribute.MatchString(s) {
			return errors.New("LDAP attribute mapping is required")
		}
	}
	if c.GroupsAttribute != "" && !ldapAttribute.MatchString(c.GroupsAttribute) {
		return errors.New("Invalid LDAP group attribute")
	}
	if c.ActiveValue == "" && c.DisabledBit == 0 {
		return errors.New("Explicit LDAP active mapping required")
	}
	if c.MaxEntries == 0 {
		c.MaxEntries = 1000
	}
	if c.MaxEntries < 1 || c.MaxEntries > 1000 {
		return errors.New("LDAP maximum entries must be 1–1000")
	}
	return nil
}
func (c *LDAPConfig) connect(ctx context.Context) (*ldap.Conn, func(), error) {
	if e := c.validate(); e != nil {
		return nil, nil, e
	}
	u, _ := url.Parse(c.URL)
	tc := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()}
	if c.CAFile != "" {
		raw, e := os.ReadFile(c.CAFile)
		if e != nil {
			return nil, nil, errors.New("LDAP CA unavailable")
		}
		roots, e := x509.SystemCertPool()
		if e != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(raw) {
			return nil, nil, errors.New("Invalid LDAP CA")
		}
		tc.RootCAs = roots
	}
	conn, e := ldap.DialURL(c.URL, ldap.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}), ldap.DialWithTLSConfig(tc))
	if e != nil {
		return nil, nil, e
	}
	conn.SetTimeout(5 * time.Second)
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	close := func() { stop(); conn.Close() }
	if u.Scheme == "ldap" {
		if e = conn.StartTLS(tc); e != nil {
			close()
			return nil, nil, e
		}
	}
	if e = conn.Bind(c.BindDN, c.BindPassword); e != nil {
		close()
		return nil, nil, e
	}
	return conn, close, nil
}
func (c *LDAPConfig) idFilter(id string) (string, error) {
	value := ldap.EscapeFilter(id)
	if c.BinaryID {
		b, e := base64.RawURLEncoding.DecodeString(id)
		if e != nil {
			return "", e
		}
		var out strings.Builder
		for _, v := range b {
			fmt.Fprintf(&out, "\\%02x", v)
		}
		value = out.String()
	}
	return "(&" + c.Filter + "(" + c.IDAttribute + "=" + value + "))", nil
}
func (c *LDAPConfig) entry(v *ldap.Entry) (DirectoryEntry, error) {
	id := v.GetAttributeValue(c.IDAttribute)
	if c.BinaryID {
		id = base64.RawURLEncoding.EncodeToString(v.GetRawAttributeValue(c.IDAttribute))
	}
	raw := v.GetAttributeValue(c.ActiveAttribute)
	if raw == "" {
		return DirectoryEntry{}, errors.New("Missing LDAP account state")
	}
	active := raw == c.ActiveValue
	if c.DisabledBit != 0 {
		n, e := strconv.ParseUint(raw, 10, 64)
		if e != nil {
			return DirectoryEntry{}, e
		}
		active = n&c.DisabledBit == 0
	}
	out := DirectoryEntry{ID: id, Name: v.GetAttributeValue(c.NameAttribute), Email: v.GetAttributeValue(c.EmailAttribute), Username: strings.ToLower(strings.TrimSpace(v.GetAttributeValue(c.UsernameAttribute))), Active: &active, Groups: v.GetAttributeValues(c.GroupsAttribute)}
	if out.Username == "" {
		return out, errors.New("Missing directory username")
	}
	if e := validateDirectoryBatch(directoryBatch{Provider: c.Provider, Entries: []DirectoryEntry{out}}); e != nil {
		return out, e
	}
	return out, nil
}
func (c *LDAPConfig) search(conn *ldap.Conn, id string) ([]*ldap.Entry, error) {
	filter := c.Filter
	if id != "" {
		var e error
		filter, e = c.idFilter(id)
		if e != nil {
			return nil, e
		}
	}
	req := ldap.NewSearchRequest(c.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, c.MaxEntries+1, 5, false, filter, []string{c.IDAttribute, c.NameAttribute, c.EmailAttribute, c.UsernameAttribute, c.ActiveAttribute, c.GroupsAttribute}, nil)
	// Bounded search fails atomically on server size/time limits; no partial sync.
	res, e := conn.Search(req)
	if e != nil {
		return nil, e
	}
	if len(res.Entries) > c.MaxEntries {
		return nil, errors.New("Directory limit exceeded")
	}
	return res.Entries, nil
}
func (c *LDAPConfig) readEntries(ctx context.Context, id string) ([]DirectoryEntry, error) {
	conn, close, e := c.connect(ctx)
	if e != nil {
		return nil, e
	}
	defer close()
	rows, e := c.search(conn, id)
	if e != nil {
		return nil, e
	}
	out := []DirectoryEntry{}
	for _, r := range rows {
		v, e := c.entry(r)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, nil
}
func (a *App) verifyLoginPassword(ctx context.Context, uid, username, password, localHash string) (bool, error) {
	var provider, did string
	if e := a.Store.DB.QueryRow(ctx, "SELECT auth_provider,directory_id FROM users WHERE id=$1", uid).Scan(&provider, &did); e != nil {
		return false, e
	}
	if provider == "local" {
		return passwordOK(password, localHash), nil
	}
	if provider != "ldap" || a.Config.LDAP == nil || !a.Config.LDAP.Enabled || len(password) == 0 || len(password) > 1024 {
		return false, nil
	}
	var id, source string
	var active bool
	if e := a.Store.DB.QueryRow(ctx, "SELECT external_id,provider,active FROM directory_entries WHERE id=$1", did).Scan(&id, &source, &active); e != nil {
		return false, nil
	}
	if !active || source != a.Config.LDAP.Provider {
		return false, nil
	}
	conn, close, e := a.Config.LDAP.connect(ctx)
	if e != nil {
		return false, appError{503, "directory_unavailable", "Directory authentication unavailable"}
	}
	defer close()
	rows, e := a.Config.LDAP.search(conn, id)
	if e != nil || len(rows) != 1 {
		return false, nil
	}
	v, e := a.Config.LDAP.entry(rows[0])
	if e != nil || v.ID != id {
		return false, nil
	}
	if !*v.Active {
		// Enrollment holds its own row lock: revoke account credentials here without
		// touching enrollment rows. The disabled directory snapshot blocks re-enrollment.
		if e = a.revokeDirectoryAccess(ctx, uid, did); e != nil {
			return false, e
		}
		return false, nil
	}
	if v.Username != username {
		return false, nil
	}
	return conn.Bind(rows[0].DN, password) == nil, nil
}

func (a *App) revokeDirectoryAccess(ctx context.Context, uid, did string) error {
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var entryID string
	if e = tx.QueryRow(ctx, "SELECT id FROM directory_entries WHERE id=$1 FOR UPDATE", did).Scan(&entryID); e != nil {
		return e
	}
	var provider string
	var admin bool
	if e = tx.QueryRow(ctx, "SELECT auth_provider,admin FROM users WHERE id=$1 FOR UPDATE", uid).Scan(&provider, &admin); e != nil {
		return e
	}
	if provider != "ldap" || admin {
		return denied()
	}
	if _, e = tx.Exec(ctx, "UPDATE directory_entries SET active=false,updated_at=now() WHERE id=$1", did); e != nil {
		return e
	}
	for _, sql := range []string{"UPDATE users SET active=false WHERE id=$1", "DELETE FROM sessions WHERE user_id=$1", "DELETE FROM api_tokens WHERE user_id=$1"} {
		if _, e = tx.Exec(ctx, sql, uid); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
