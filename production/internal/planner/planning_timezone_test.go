package planner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type timezoneTestTransport func(*http.Request) (*http.Response, error)

func (f timezoneTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPlanningTimezoneProviderRegression(t *testing.T) {
	var published map[string]any
	g := GraphAdapter{Enabled: true, Client: &http.Client{Transport: timezoneTestTransport(func(r *http.Request) (*http.Response, error) {
		body := `{}`
		if r.Method == "GET" {
			body = `{"value":[{"alias":"America/New_York"},{"alias":"Europe/Moscow"}]}`
		} else if err := json.NewDecoder(r.Body).Decode(&published); err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	_, err := g.Publish(context.Background(), BusyBlock{ID: "synthetic-zone", Date: "2027-01-04", Start: "09:00", End: "10:00", Timezone: "America/New_York"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := published["start"].(map[string]any)["timeZone"]; got != "America/New_York" {
		t.Fatalf("busy publication ignored declared IANA zone: got %v", got)
	}
}
