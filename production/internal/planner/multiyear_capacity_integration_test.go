package planner

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Exercise real HTTP/DB portfolio calculation with different verified annual
// versions. The first project observes New Year as a holiday; the second uses
// an explicitly different synthetic calendar in which January 1 is working.
func testMultiYearSharedCapacityAPI(t *testing.T, store *Store, call func(int, string, string, any, *testSession) json.RawMessage, admin *testSession, c26, holiday27 Calendar) {
	t.Helper()
	ctx := context.Background()
	decode := func(raw json.RawMessage) Project {
		t.Helper()
		var p Project
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	get := func(id string) Project {
		t.Helper()
		var out struct{ Project Project }
		if err := json.Unmarshal(call(200, "GET", "/projects/"+id, nil, admin), &out); err != nil {
			t.Fatal(err)
		}
		return out.Project
	}
	save := func(p Project) Project {
		t.Helper()
		return decode(call(200, "PUT", "/projects/"+p.ID, saveRequest{Project: p}, admin))
	}
	employee := Employee{ID: "multiyear-shared-employee", Name: "Synthetic multi-year employee", Daily: 480}
	call(200, "POST", "/employees", employee, admin)
	working27 := annualFixture(t, 2027)
	working27.ID = "synthetic-2027-shared-working-january"
	recalculateChecksum(t, &working27)
	call(201, "POST", "/calendars", working27, admin)
	create := func(title, anchor string, pins map[string]string) Project {
		t.Helper()
		p := decode(call(201, "POST", "/projects", map[string]any{"title": title, "start": "2026-12-31", "duration_mode": "working", "calendar_id": anchor, "calendar_versions": pins}, admin))
		p.People = []Participation{{EmployeeID: employee.ID, Week: testParticipationWeek([]int{1, 2, 3, 4, 5}, 240, "09:00", "18:00"), Exceptions: map[string]Activity{}}}
		return p
	}
	first := create("Synthetic holiday capacity project", c26.ID, map[string]string{"2027": holiday27.ID})
	first.People[0].Exceptions["2027-01-04"] = Activity{Active: true, Start: "09:00", End: "18:00", Daily: 360}
	first.Tasks = []Task{{ID: "holiday-capacity-task", Title: "Holiday capacity task", Status: "todo", Responsibility: "internal", RequestedStart: "2026-12-31", Planned: 600, Assignments: []Assignment{{EmployeeID: employee.ID, Daily: 360}}}}
	first = save(first)
	second := create("Synthetic working-day capacity project", working27.ID, map[string]string{"2026": c26.ID})
	second.Tasks = []Task{{ID: "working-capacity-task", Title: "Working-day capacity task", Status: "todo", Responsibility: "internal", RequestedStart: "2026-12-31", Planned: 480, Assignments: []Assignment{{EmployeeID: employee.ID, Daily: 240}}}}
	second = save(second)
	defer func() {
		for _, id := range []string{first.ID, second.ID} {
			p := get(id)
			call(200, "DELETE", "/projects/"+id, map[string]int64{"revision": p.Revision}, admin)
		}
	}()
	firstLoads := []Load{{employee.ID, first.Tasks[0].ID, "2026-12-31", 240}, {employee.ID, first.Tasks[0].ID, "2027-01-04", 360}}
	secondLoads := []Load{{employee.ID, second.Tasks[0].ID, "2026-12-31", 240}, {employee.ID, second.Tasks[0].ID, "2027-01-01", 240}}
	assertReady := func(p Project, expected []Load, end string) {
		t.Helper()
		if p.CalculationState != "ready" || len(p.ProvisionalYears) != 0 || p.Tasks[0].Start != "2026-12-31" || p.Tasks[0].End != end || !reflect.DeepEqual(p.Loads, expected) {
			t.Fatal("cross-year portfolio did not use each project's annual calendar and quota", p.CalculationState, p.ProvisionalYears, p.Tasks[0], p.Loads, expected)
		}
	}
	assertReady(get(first.ID), firstLoads, "2027-01-04")
	assertReady(get(second.ID), secondLoads, "2027-01-01")
	// 360 + 120 minutes on January 4 exactly meets the global eight-hour cap.
	second.Tasks[0].Planned = 600
	second = save(second)
	secondLoads = append(secondLoads, Load{employee.ID, second.Tasks[0].ID, "2027-01-04", 120})
	assertReady(second, secondLoads, "2027-01-04")
	assertReady(get(first.ID), firstLoads, "2027-01-04")
	assertReady(get(second.ID), secondLoads, "2027-01-04")
	// Import's existing employee-mapping boundary stays intact.
	withEmployees := call(200, "GET", "/projects/"+first.ID+"/export", nil, admin)
	call(422, "POST", "/import", map[string]any{"project": json.RawMessage(withEmployees)}, admin)
	// Raising real work adds 240 minutes on January 4: 360 + 240 = 600 > 480.
	// December 31 remains exactly 480; a loader using the wrong project's year
	// would allocate the holiday project's hours on January 1 and miss this conflict.
	second.Tasks[0].Planned = 720
	second = save(second)
	assertBlocked := func(p Project) {
		t.Helper()
		warning := strings.Join(p.Warnings, " ")
		if p.CalculationState != "blocked_capacity" || len(p.Loads) != 0 || p.Tasks[0].Start != "" || p.Tasks[0].End != "" || !strings.Contains(warning, "2027-01-04") || !strings.Contains(warning, "600") || !strings.Contains(warning, "480") {
			t.Fatal("cross-year actual global overload was not withheld explicitly", p.CalculationState, p.Tasks[0], p.Loads, warning)
		}
	}
	assertBlocked(second)
	before, err := readProject(ctx, store.DB, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertBlocked(get(first.ID))
	assertBlocked(get(second.ID))
	after, err := readProject(ctx, store.DB, first.ID)
	if err != nil || !reflect.DeepEqual(before, after) || after.Tasks[0].End != "2027-01-04" {
		t.Fatal("capacity refresh mutated the stored multi-year project", err, before, after)
	}
	// Releasing only the excess actual hours restores both projects without
	// changing either project's participation quota or the other project's revision.
	second.Tasks[0].Planned = 600
	second = save(second)
	assertReady(second, secondLoads, "2027-01-04")
	restored := get(first.ID)
	assertReady(restored, firstLoads, "2027-01-04")
	if restored.Revision != before.Revision {
		t.Fatal("read-only cross-project recalculation changed the project revision")
	}
}
