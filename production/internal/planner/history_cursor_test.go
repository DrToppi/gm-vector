package planner

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestHistoryCursorIntegrityAndContext(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	key := random(32)
	u := Principal{User: User{ID: "synthetic-user"}}
	f := HistoryFilters{TaskID: "root", DateFrom: "1999-01-01"}
	c := historyCursor{Version: 1, Project: "project", Principal: u.ID, Auth: cursorAuth(u), Filters: f, Limit: 100, Order: historyOrder, Revision: 7, LastDate: "2024-02-29", LastID: "entry", Issued: now.Unix(), Expires: now.Add(historyCursorTTL).Unix()}
	signed, e := encodeHistoryCursor(c, key)
	if e != nil {
		t.Fatal(e)
	}
	got, e := decodeHistoryCursor(signed, key, "project", u, f, 100, now)
	if e != nil || got != c {
		t.Fatal("valid cursor", got, e)
	}
	tests := []struct {
		name, value, project string
		principal            Principal
		filters              HistoryFilters
		limit                int
		key                  []byte
	}{
		{"tampered", "A" + signed[1:], "project", u, f, 100, key},
		{"oversized", strings.Repeat("A", historyCursorMaxBytes+1), "project", u, f, 100, key},
		{"foreign-project", signed, "other", u, f, 100, key},
		{"foreign-principal", signed, "project", Principal{User: User{ID: "other"}}, f, 100, key},
		{"other-auth", signed, "project", Principal{User: u.User, Token: true}, f, 100, key},
		{"changed-filters", signed, "project", u, HistoryFilters{TaskID: "other"}, 100, key},
		{"changed-limit", signed, "project", u, f, 500, key},
		{"rotated-key", signed, "project", u, f, 100, random(32)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := decodeHistoryCursor(test.value, test.key, test.project, test.principal, test.filters, test.limit, now)
			var ae appError
			if !errors.As(err, &ae) || ae.Code != "invalid_cursor" || ae.Status != 422 {
				t.Fatal("invalid context accepted", err)
			}
		})
	}
	wrongOrder := c
	wrongOrder.Order = "date_asc"
	signed, _ = encodeHistoryCursor(wrongOrder, key)
	if _, e = decodeHistoryCursor(signed, key, "project", u, f, 100, now); e == nil {
		t.Fatal("unsupported order accepted")
	}
}

func TestHistoryCursorFixedExpiry(t *testing.T) {
	now := time.Date(2026, 10, 30, 23, 59, 0, 0, time.UTC)
	key := random(32)
	u := Principal{User: User{ID: "user"}}
	f := HistoryFilters{}
	c := historyCursor{Version: 1, Project: "p", Principal: u.ID, Auth: cursorAuth(u), Filters: f, Limit: 100, Order: historyOrder, Revision: 1, LastDate: "2026-01-01", LastID: "entry", Issued: now.Unix(), Expires: now.Add(historyCursorTTL).Unix()}
	signed, _ := encodeHistoryCursor(c, key)
	for _, zone := range []string{"UTC", "Europe/Moscow", "America/New_York", "Asia/Kathmandu"} {
		_, loc, e := resolvePlanningTimezone(zone)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = decodeHistoryCursor(signed, key, "p", u, f, 100, now.Add(historyCursorTTL-time.Second).In(loc)); e != nil {
			t.Fatal("zone changed UTC TTL", zone, e)
		}
	}
	_, e := decodeHistoryCursor(signed, key, "p", u, f, 100, now.Add(historyCursorTTL))
	var ae appError
	if !errors.As(e, &ae) || ae.Code != "cursor_expired" {
		t.Fatal("expiry boundary", e)
	}
	c.LastID = "next-entry"
	next, _ := encodeHistoryCursor(c, key)
	decoded, e := decodeHistoryCursor(next, key, "p", u, f, 100, now.Add(time.Minute))
	if e != nil || decoded.Expires != now.Add(historyCursorTTL).Unix() {
		t.Fatal("continuation extended expiry", e, decoded.Expires)
	}
}
