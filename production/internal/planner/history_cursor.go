package planner

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"time"
)

const historyOrder = "work_date_desc_id_asc"
const historyCursorTTL = 15 * time.Minute
const historyCursorMaxBytes = 4096

type historyCursor struct {
	Version   int            `json:"v"`
	Project   string         `json:"project"`
	Principal string         `json:"principal"`
	Auth      string         `json:"auth"`
	Filters   HistoryFilters `json:"filters"`
	Limit     int            `json:"limit"`
	Order     string         `json:"order"`
	Revision  int64          `json:"revision"`
	LastDate  string         `json:"date"`
	LastID    string         `json:"id"`
	Issued    int64          `json:"issued"`
	Expires   int64          `json:"expires"`
}

func invalidHistoryCursor() error {
	return appError{422, "invalid_cursor", "Invalid history cursor; restart the list"}
}
func cursorAuth(u Principal) string {
	if u.Token {
		return "token"
	}
	return "session"
}
func historyCursorKey(secret []byte) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte("gmVector:time-history-cursor:v1"))
	return m.Sum(nil)
}
func encodeHistoryCursor(c historyCursor, secret []byte) (string, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, historyCursorKey(secret))
	m.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil)), nil
}
func decodeHistoryCursor(value string, secret []byte, project string, u Principal, filters HistoryFilters, limit int, now time.Time) (historyCursor, error) {
	var c historyCursor
	if len(value) == 0 || len(value) > historyCursorMaxBytes {
		return c, invalidHistoryCursor()
	}
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return c, invalidHistoryCursor()
	}
	raw, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil {
		return c, invalidHistoryCursor()
	}
	sig, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return c, invalidHistoryCursor()
	}
	m := hmac.New(sha256.New, historyCursorKey(secret))
	m.Write(raw)
	if !hmac.Equal(sig, m.Sum(nil)) {
		return c, invalidHistoryCursor()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || d.Decode(&struct{}{}) != io.EOF {
		return c, invalidHistoryCursor()
	}
	if c.Version != 1 || c.Project != project || c.Principal != u.ID || c.Auth != cursorAuth(u) || c.Filters != filters || c.Limit != limit || c.Order != historyOrder || c.LastID == "" || len(c.LastID) > 80 || c.LastDate == "" || c.Expires-c.Issued != int64(historyCursorTTL/time.Second) || c.Issued > now.Unix() {
		return c, invalidHistoryCursor()
	}
	if _, e = date(c.LastDate); e != nil {
		return c, invalidHistoryCursor()
	}
	if now.Unix() >= c.Expires {
		return c, appError{422, "cursor_expired", "History cursor expired; restart the list"}
	}
	return c, nil
}
