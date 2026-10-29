package planner

import (
	"encoding/base64"
	"encoding/json"
	"github.com/go-ldap/ldap/v3"
	"strings"
	"testing"
)

func fixtureLDAP() LDAPConfig {
	return LDAPConfig{Enabled: true, URL: "ldaps://directory.example.test:636", Provider: "corp", BindDN: "cn=reader,dc=example", BindPassword: "synthetic-only", BaseDN: "dc=example", Filter: "(objectClass=person)", IDAttribute: "entryUUID", NameAttribute: "displayName", EmailAttribute: "mail", UsernameAttribute: "uid", ActiveAttribute: "enabled", ActiveValue: "TRUE", MaxEntries: 1000}
}
func TestLDAPTransportAndMapping(t *testing.T) {
	c := fixtureLDAP()
	if e := c.validate(); e != nil {
		t.Fatal(e)
	}
	for _, url := range []string{"ldap://directory.example.test", "http://directory.example.test", "ldaps://name:password@directory.example.test", "ldaps://directory.example.test/path"} {
		v := c
		v.URL = url
		if v.validate() == nil {
			t.Fatalf("accepted unsafe URL %s", url)
		}
	}
	c.URL = "ldap://directory.example.test"
	c.StartTLS = true
	if e := c.validate(); e != nil {
		t.Fatal(e)
	}
	c.ActiveAttribute = ""
	if c.validate() == nil {
		t.Fatal("missing explicit lifecycle accepted")
	}
}
func TestLDAPStableFilterEscaping(t *testing.T) {
	c := fixtureLDAP()
	f, e := c.idFilter("x*)(uid=*)")
	if e != nil || strings.Contains(f, "(uid=*)") {
		t.Fatalf("filter injection: %q %v", f, e)
	}
	if _, e = ldap.CompileFilter(f); e != nil {
		t.Fatal(e)
	}
	c.BinaryID = true
	f, e = c.idFilter(base64.RawURLEncoding.EncodeToString([]byte{0, 42, 255}))
	if e != nil || !strings.Contains(f, `\00\2a\ff`) {
		t.Fatalf("binary identity %q %v", f, e)
	}
}
func TestLDAPDisabledMappingAndRequiredIdentity(t *testing.T) {
	c := fixtureLDAP()
	entry := ldap.NewEntry("cn=Test,dc=example", map[string][]string{"entryUUID": {"stable-1"}, "displayName": {"Synthetic User"}, "mail": {"user@example.test"}, "uid": {"synthetic.user"}, "enabled": {"FALSE"}})
	v, e := c.entry(entry)
	if e != nil || *v.Active {
		t.Fatalf("disabled entry %v", e)
	}
	c.ActiveAttribute = "userAccountControl"
	c.DisabledBit = 2
	entry.Attributes = append(entry.Attributes, &ldap.EntryAttribute{Name: "userAccountControl", Values: []string{"514"}})
	v, e = c.entry(entry)
	if e != nil || *v.Active {
		t.Fatalf("AD disabled mapping %v", e)
	}
	entry.Attributes[0].Values = nil
}
func TestDirectoryScopesAndValidation(t *testing.T) {
	u := Principal{User: User{Admin: true}, Token: true, Scopes: []string{"project:write"}}
	if directoryAllowed(u, "directory:write") {
		t.Fatal("project scope broadened")
	}
	u.Scopes = []string{"directory:write"}
	if !directoryAllowed(u, "directory:write") {
		t.Fatal("explicit scope denied")
	}
	active := true
	b := directoryBatch{Provider: "external", Entries: []DirectoryEntry{{ID: "stable", Name: "Synthetic", Active: &active}}}
	if e := validateDirectoryBatch(b); e != nil {
		t.Fatal(e)
	}
	b.Entries = append(b.Entries, b.Entries[0])
	if validateDirectoryBatch(b) == nil {
		t.Fatal("duplicate accepted")
	}
	b.Entries = b.Entries[:1]
	b.Entries[0].Active = nil
	if validateDirectoryBatch(b) == nil {
		t.Fatal("implicit deactivation accepted")
	}
}

func TestNeutralImportReferenceBoundary(t *testing.T) {
	var v ImportRequest
	if e := strictJSON([]byte(`{"project":{"id":"p","external_reference":"EXT-123"},"preview":true}`), &v); e != nil {
		t.Fatal(e)
	}
	if v.Project.ExternalReference != "EXT-123" || !v.Preview {
		t.Fatal("neutral reference not preserved")
	}
	for _, raw := range []string{`{"project":{"unknown":"value"}}`, `{"project":{"id":"p"},"unexpected":true}`} {
		if e := strictJSON([]byte(raw), &v); e == nil {
			t.Fatal("unknown import field accepted")
		}
	}
	raw, _ := json.Marshal(v.Project)
	if !strings.Contains(string(raw), `"external_reference"`) {
		t.Fatal("neutral reference missing from export")
	}
}
