package planner

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPlanningTimezoneIntegration(t *testing.T) {
	f := newReadPathFixture(t, 2)
	ctx := context.Background()
	if _, e := f.store.DB.Exec(ctx, "UPDATE users SET admin=true WHERE id='profile-user-09'"); e != nil {
		t.Fatal(e)
	}
	apps := map[string]*App{defaultPlanningTimezone: f.app}
	for _, zone := range []string{"UTC", "America/New_York", "Asia/Kathmandu"} {
		config := f.app.Config
		config.PlanningTimezone = zone
		a, e := NewApp(f.store, config)
		if e != nil {
			t.Fatal(e)
		}
		apps[zone] = a
	}
	for zone, a := range apps {
		metadata := historyDecode[map[string]json.RawMessage](t, historyCall(t, f, a.Handler(), "GET", "/auth/me", nil, 0), 200)
		var got string
		if e := json.Unmarshal(metadata["planning_timezone"], &got); e != nil || got != zone {
			t.Fatal("App metadata clock leaked across a shared Store", zone, got, e)
		}
	}
	for _, zone := range []string{"Local", "Etc/DefinitelyMissing"} {
		config := f.app.Config
		config.PlanningTimezone = zone
		if _, e := NewApp(f.store, config); e == nil {
			t.Fatal("invalid planning timezone started", zone)
		}
	}
	instant := time.Date(2026, 11, 1, 21, 30, 0, 0, time.UTC)
	for _, zone := range []string{"America/New_York", defaultPlanningTimezone} {
		a := apps[zone]
		phase := "direct-import-" + zone
		observed := 0
		f.trace.barrier = func(queryCtx context.Context, _ string) {
			if queryCtx.Value(readPathPhaseKey{}) != phase {
				return
			}
			clock, ok := queryCtx.Value(planningClockKey{}).(planningClock)
			if !ok || clock.zone != zone || !clock.now.Equal(instant) {
				t.Errorf("direct App.Import used another policy/instant: %+v", clock)
			}
			observed++
		}
		p := Project{ID: newID(), Title: "Synthetic direct-clock import", Start: "2026-10-30", Mode: "working", CalendarID: f.projects[0].CalendarID, CalendarVersions: map[string]string{}, People: []Participation{}, Tasks: []Task{{ID: "done", Title: "Historical date", Status: "done", Responsibility: "internal", RequestedStart: "2026-10-30", Start: "2026-10-30", End: "2026-10-30", Assignments: []Assignment{}}, {ID: "next", Title: "Next working date", Status: "todo", Responsibility: "internal", RequestedStart: "2026-10-30", DurationDays: 1, Assignments: []Assignment{}}}, Links: []Link{{From: "done", To: "next", Mode: "after"}}}
		callCtx := context.WithValue(ctx, readPathPhaseKey{}, phase)
		callCtx = context.WithValue(callCtx, planningClockKey{}, planningClock{now: instant, zone: "UTC"})
		if e := a.Import(callCtx, p, "profile-user-00", "synthetic-clock-import"); e != nil {
			t.Fatal(e)
		}
		if observed == 0 {
			t.Fatal("direct import was not observed")
		}
		stored, e := readProject(ctx, f.store.DB, p.ID)
		if e != nil || stored.Tasks[0].Start != "2026-10-30" || stored.Tasks[0].End != "2026-10-30" || stored.Tasks[1].Start != "2026-11-02" || stored.Tasks[1].End != "2026-11-02" {
			t.Fatal("planning clock rewrote date labels", stored.Tasks, e)
		}
	}
	// The same immutable request clock reaches other projects' preparation.
	otherHeaders := 0
	f.trace.barrier = func(queryCtx context.Context, sql string) {
		if queryCtx.Value(readPathPhaseKey{}) != "portfolio-clock" {
			return
		}
		clock, ok := queryCtx.Value(planningClockKey{}).(planningClock)
		if !ok || clock.zone != "America/New_York" || !clock.now.Equal(instant) {
			t.Errorf("portfolio uses mixed planning clocks: %+v", clock)
		}
		if strings.HasPrefix(sql, "SELECT id,title,brief,start_date::text") {
			otherHeaders++
		}
	}
	p, e := readProject(ctx, f.store.DB, f.projects[0].ID)
	if e != nil {
		t.Fatal(e)
	}
	callCtx := context.WithValue(ctx, readPathPhaseKey{}, "portfolio-clock")
	callCtx = context.WithValue(callCtx, planningClockKey{}, planningClock{now: instant, zone: "UTC"})
	if e = apps["America/New_York"].prepareProject(callCtx, f.store.DB, &p); e != nil {
		t.Fatal(e)
	}
	if otherHeaders == 0 || p.CalculationState != "ready" || len(p.Loads) != 600 {
		t.Fatal("related portfolio fixture not exercised", otherHeaders, p.CalculationState, len(p.Loads))
	}
	f.trace.barrier = nil
	audit := historyDecode[[]struct {
		CreatedAt string `json:"created_at"`
	}](t, historyCall(t, f, apps["Asia/Kathmandu"].Handler(), "GET", "/audit", nil, 9), 200)
	if len(audit) == 0 {
		t.Fatal("missing synthetic audit")
	}
	for _, event := range audit {
		if !strings.HasSuffix(event.CreatedAt, "Z") {
			t.Fatal("planning zone changed the API instant", event.CreatedAt)
		}
	}
}
