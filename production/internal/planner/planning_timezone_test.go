package planner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
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

func TestPlanningTimezoneCivilDatesAndDST(t *testing.T) {
	for _, zone := range []string{"", "UTC", "Europe/Moscow", "America/New_York", "Asia/Kathmandu"} {
		name, loc, e := resolvePlanningTimezone(zone)
		if e != nil || loc == nil || name == "" {
			t.Fatal(zone, e)
		}
	}
	for _, zone := range []string{"Local", "localtime", "posixrules", "Etc/DefinitelyMissing", "../zone"} {
		if _, _, e := resolvePlanningTimezone(zone); e == nil {
			t.Fatal("invalid zone accepted", zone)
		}
	}
	instant := time.Date(2026, 10, 30, 21, 30, 0, 0, time.UTC)
	for zone, want := range map[string]string{"UTC": "2026-10-30", "Europe/Moscow": "2026-10-31", "America/New_York": "2026-10-30", "Asia/Kathmandu": "2026-10-31"} {
		a := &App{Config: Config{PlanningTimezone: zone}}
		ctx := context.WithValue(context.Background(), planningClockKey{}, planningClock{now: instant, zone: "UTC"})
		ctx, e := a.withPlanningClock(ctx)
		if e != nil {
			t.Fatal(e)
		}
		n, e := planningNow(ctx)
		if e != nil || fmtDate(n) != want || !n.Equal(instant) {
			t.Fatal("civil day/instant", zone, n, want, e)
		}
	}
	a, e := date("2027-03-13")
	if e != nil {
		t.Fatal(e)
	}
	b, e := date("2027-03-15")
	if e != nil || int(b.Sub(a).Hours()/24) != 2 {
		t.Fatal("date-only arithmetic changed", a, b, e)
	}
	for _, test := range []struct {
		day, start, end, zone string
		unsafe                bool
	}{
		{"2027-03-14", "01:00", "04:00", "America/New_York", true},
		{"2027-03-14", "02:15", "02:45", "America/New_York", true},
		{"2027-11-07", "01:15", "01:45", "America/New_York", true},
		{"2027-03-14", "09:00", "18:00", "America/New_York", false},
		{"2027-03-14", "01:00", "04:00", "UTC", false},
		{"2027-03-14", "01:00", "04:00", "Asia/Kathmandu", false},
	} {
		if e := validateWallWindow(test.day, test.start, test.end, test.zone); (e != nil) != test.unsafe {
			t.Fatal("gap/fold/interior transition", test, e)
		}
	}
	p, _, _ := fixture()
	p.CalculationState = "ready"
	p.People[0].Week["0"] = Activity{Active: true, Start: "01:00", End: "04:00", Daily: 180}
	p.Loads = []Load{{EmployeeID: "e", TaskID: "a", Date: "2027-03-14", Minutes: 180}}
	blocks, warnings := BuildBusyBlocks([]Project{p}, "America/New_York")
	if len(blocks) != 0 || len(warnings) == 0 {
		t.Fatal("unsafe busy publication was not withheld", blocks, warnings)
	}
}
