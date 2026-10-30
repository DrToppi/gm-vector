package planner

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestPortableProjectIntegration(t *testing.T) {
	f := newReadPathFixture(t, 1)
	ctx := context.Background()
	if _, e := f.store.DB.Exec(ctx, "UPDATE users SET admin=true WHERE id='profile-user-09'"); e != nil {
		t.Fatal(e)
	}
	h := f.app.Handler()
	p := historyDecode[Project](t, historyCall(t, f, h, "POST", "/projects", map[string]any{"title": "Synthetic portable project", "start": "2026-12-31", "calendar_id": f.projects[0].CalendarID, "calendar_versions": f.projects[0].CalendarVersions, "duration_mode": "working"}, 9), 201)
	p.Tasks = []Task{{ID: "portable-a", Title: "Portable predecessor", Status: "todo", RequestedStart: "2026-12-31", Responsibility: "internal", DurationDays: 1, Assignments: []Assignment{}}, {ID: "portable-b", Title: "Portable successor", Status: "todo", RequestedStart: "2026-12-31", Responsibility: "internal", DurationDays: 1, Assignments: []Assignment{}}}
	p.Links = []Link{{From: "portable-a", To: "portable-b", Mode: "after"}}
	p.ExternalReference = "synthetic-external-reference"
	path := "/projects/" + p.ID
	p = historyDecode[Project](t, historyCall(t, f, h, "PUT", path, saveRequest{Project: p}, 9), 200)
	if p.Tasks[0].End != "2026-12-31" || p.Tasks[1].End != "2027-01-04" {
		t.Fatal("independent holiday/dependency fixture", p.Tasks)
	}
	legacy := historyDecode[map[string]json.RawMessage](t, historyCall(t, f, h, "GET", path+"/export", nil, 9), 200)
	for _, field := range []string{"metadata", "format", "time_entries"} {
		if _, ok := legacy[field]; ok {
			t.Fatal("legacy Project export changed", field)
		}
	}
	doc := historyDecode[PortableProject](t, historyCall(t, f, h, "GET", path+"/portable/export", nil, 9), 200)
	if doc.Format != "gm-vector-project" || doc.SchemaVersion != 1 || doc.Metadata.PlanningTimezone != defaultPlanningTimezone || doc.Metadata.GeneratedAt == "" || doc.Project.ID != p.ID {
		t.Fatal("portable document", doc.Metadata)
	}
	config := f.app.Config
	config.PlanningTimezone = "America/New_York"
	ny, e := NewApp(f.store, config)
	if e != nil {
		t.Fatal(e)
	}
	nyHandler := ny.Handler()
	for _, test := range []struct {
		handlerZone string
		handler     *App
	}{{defaultPlanningTimezone, f.app}, {"America/New_York", ny}} {
		metadata := historyDecode[map[string]json.RawMessage](t, historyCall(t, f, test.handler.Handler(), "GET", "/auth/me", nil, 9), 200)
		var zone string
		json.Unmarshal(metadata["planning_timezone"], &zone)
		if zone != test.handlerZone {
			t.Fatal("another App mutated the shared Store planning clock", zone, test.handlerZone)
		}
	}
	count := func() int {
		var n int
		if e := f.store.DB.QueryRow(ctx, "SELECT count(*) FROM projects").Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	before := count()
	preview := historyDecode[struct {
		Project  Project `json:"project"`
		Source   string  `json:"source_planning_timezone"`
		Target   string  `json:"target_planning_timezone"`
		Required bool    `json:"timezone_acknowledgement_required"`
	}](t, historyCall(t, f, nyHandler, "POST", "/import/portable", map[string]any{"document": doc, "preview": true}, 9), 200)
	if !preview.Required || preview.Source != defaultPlanningTimezone || preview.Target != "America/New_York" || count() != before || preview.Project.ExternalReference != "" {
		t.Fatal("mismatch preview persisted or omitted acknowledgement context", preview)
	}
	historyErrorCode(t, historyCall(t, f, nyHandler, "POST", "/import/portable", map[string]any{"document": doc, "preview": false}, 9), 422, "timezone_acknowledgement_required")
	for _, isPreview := range []bool{true, false} {
		historyErrorCode(t, historyCall(t, f, nyHandler, "POST", "/import/portable", map[string]any{"document": doc, "preview": isPreview, "timezone_acknowledgement": map[string]string{"source": defaultPlanningTimezone, "target": "Asia/Kathmandu"}}, 9), 422, "timezone_acknowledgement_invalid")
	}
	if count() != before {
		t.Fatal("unacknowledged or stale acknowledgement imported a project")
	}
	imported := historyDecode[Project](t, historyCall(t, f, nyHandler, "POST", "/import/portable", map[string]any{"document": doc, "preview": false, "timezone_acknowledgement": map[string]string{"source": defaultPlanningTimezone, "target": "America/New_York"}}, 9), 201)
	if imported.ID == p.ID || imported.Revision != 0 || imported.ExternalReference != "" || len(imported.People) != 0 || !reflect.DeepEqual(imported.Tasks, doc.Project.Tasks) || !reflect.DeepEqual(imported.Links, doc.Project.Links) || !reflect.DeepEqual(imported.CalendarVersions, doc.Project.CalendarVersions) {
		t.Fatal("portable import changed identity policy or dates", doc.Project, imported)
	}
	matching := historyDecode[Project](t, historyCall(t, f, h, "POST", "/import/portable", map[string]any{"document": doc, "preview": false}, 9), 201)
	if matching.Tasks[1].End != "2027-01-04" {
		t.Fatal("matching-zone import changed dates")
	}
	// A caller cannot use the portable wrapper to evade established restrictions.
	withPeople := doc
	withPeople.Project = f.projects[0]
	historyErrorCode(t, historyCall(t, f, h, "POST", "/import/portable", map[string]any{"document": withPeople, "preview": true}, 9), 422, "validation")
	historyErrorCode(t, historyCall(t, f, h, "POST", "/import", map[string]any{"document": doc, "preview": true}, 9), 422, "validation")
	historyErrorCode(t, historyCall(t, f, h, "POST", "/import/portable", map[string]any{"document": doc}, 9), 422, "validation")
	malformed := doc
	malformed.SchemaVersion = 2
	historyErrorCode(t, historyCall(t, f, h, "POST", "/import/portable", map[string]any{"document": malformed, "preview": true}, 9), 422, "validation")
	raw, _ := json.Marshal(doc)
	var unknown map[string]any
	json.Unmarshal(raw, &unknown)
	unknown["metadata"].(map[string]any)["unknown_field"] = true
	historyErrorCode(t, historyCall(t, f, h, "POST", "/import/portable", map[string]any{"document": unknown, "preview": true}, 9), 422, "validation")
	oversized := doc
	oversized.Project.Brief = strings.Repeat("x", 5<<20)
	historyErrorCode(t, historyCall(t, f, h, "POST", "/import/portable", map[string]any{"document": oversized, "preview": true}, 9), 413, "too_large")
	// Legacy files stay usable on another configured installation, without an
	// invented provenance field or a new acknowledgement request shape.
	oldPreview := historyDecode[map[string]json.RawMessage](t, historyCall(t, f, nyHandler, "POST", "/import", map[string]any{"project": doc.Project, "preview": true}, 9), 200)
	if _, ok := oldPreview["source_planning_timezone"]; ok {
		t.Fatal("legacy provenance was invented")
	}
	var facts int
	if e := f.store.DB.QueryRow(ctx, "SELECT count(*) FROM time_entries").Scan(&facts); e != nil || facts != 0 {
		t.Fatal("project import invented time facts", facts, e)
	}
}
