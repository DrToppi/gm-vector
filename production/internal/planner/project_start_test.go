package planner

import (
	"encoding/json"
	"reflect"
	"testing"
)

func copyStartProject(t *testing.T, p Project) Project {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var clone Project
	if err = json.Unmarshal(raw, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func startTask(t *testing.T, p Project, id string) Task {
	t.Helper()
	for _, task := range p.Tasks {
		if task.ID == id {
			return task
		}
	}
	t.Fatalf("missing task %s", id)
	return Task{}
}

func startShiftFixture(t *testing.T) (Project, Calendar, map[string]Employee) {
	t.Helper()
	p, cal, employees := fixture()
	p.Tasks[0].Parent, p.Tasks[0].Status = "group", "doing"
	p.Tasks[0].Actual = 45
	p.Tasks = append(p.Tasks,
		Task{ID: "group", Title: "Summary", Responsibility: "internal", Status: "todo"},
		Task{ID: "done", Parent: "group", Title: "Finished", Responsibility: "internal", Status: "done", RequestedStart: "2026-10-20", Start: "2026-10-20", End: "2026-10-21", Planned: 120, Actual: 150, LegacyActual: 30, LegacyStart: "2026-09-10", LegacyEnd: "2026-09-11"},
		Task{ID: "customer", Parent: "group", Title: "Customer", Responsibility: "customer", Status: "todo", RequestedStart: "2026-10-23", Start: "2026-10-23", End: "2026-10-26", CustomerDue: "2026-11-30"},
	)
	if err := Calculate(&p, cal, employees, schedulingNow); err != nil {
		t.Fatal(err)
	}
	return p, cal, employees
}

func TestProjectStartShiftBothDirectionsAndAllStatuses(t *testing.T) {
	for _, tc := range []struct {
		name, projectStart, activeStart, activeEnd, doneStart, doneEnd, customerEnd string
	}{
		{"forward", "2026-10-30", "2026-10-30", "2026-11-03", "2026-10-27", "2026-10-28", "2026-11-02"},
		{"backward", "2026-10-16", "2026-10-16", "2026-10-20", "2026-10-13", "2026-10-14", "2026-10-19"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old, cal, employees := startShiftFixture(t)
			unchanged := copyStartProject(t, old)
			p := copyStartProject(t, old)
			p.Start = tc.projectStart
			if err := shiftProjectStart(old, &p); err != nil {
				t.Fatal(err)
			}
			if err := Calculate(&p, cal, employees, schedulingNow); err != nil {
				t.Fatal(err)
			}
			active, done, customer, summary := startTask(t, p, "a"), startTask(t, p, "done"), startTask(t, p, "customer"), startTask(t, p, "group")
			if active.RequestedStart != tc.activeStart || active.Start != tc.activeStart || active.End != tc.activeEnd || active.Status != "doing" || active.Planned != 600 || active.Actual != 45 {
				t.Fatalf("active task dates/effort/status: %+v", active)
			}
			if done.Start != tc.doneStart || done.End != tc.doneEnd || done.RequestedStart != tc.doneStart || done.Status != "done" || done.Planned != 120 || done.Actual != 150 || done.LegacyActual != 30 || done.LegacyStart != "2026-09-10" || done.LegacyEnd != "2026-09-11" {
				t.Fatalf("completed task dates/history/effort: %+v", done)
			}
			if customer.Start != tc.activeStart || customer.End != tc.customerEnd || customer.CustomerDue != "2026-11-30" {
				t.Fatalf("customer range/control date: %+v", customer)
			}
			if !summary.Summary || summary.Start != tc.doneStart || summary.End != tc.activeEnd || summary.Planned != 720 || summary.Actual != 195 || summary.Status != "doing" {
				t.Fatalf("summary must roll up shifted children: %+v", summary)
			}
			if !reflect.DeepEqual(old, unchanged) {
				t.Fatal("shift mutated the persisted baseline")
			}
		})
	}
}

func TestProjectStartShiftPreviewIdempotentAndEmptyAnchor(t *testing.T) {
	old, cal, employees := startShiftFixture(t)
	old.Tasks[0].RequestedStart = ""
	p := copyStartProject(t, old)
	p.Start = "2026-10-30"
	if err := shiftProjectStart(old, &p); err != nil {
		t.Fatal(err)
	}
	if err := Calculate(&p, cal, employees, schedulingNow); err != nil {
		t.Fatal(err)
	}
	if got := startTask(t, p, "a"); got.RequestedStart != "" || got.Start != "2026-10-30" {
		t.Fatalf("empty anchor follows the project start: %+v", got)
	}
	preview := copyStartProject(t, p)
	for i := 0; i < 2; i++ {
		if err := shiftProjectStart(old, &p); err != nil {
			t.Fatal(err)
		}
		if err := Calculate(&p, cal, employees, schedulingNow); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(p, preview) {
			t.Fatalf("repeat %d shifted the preview again", i+1)
		}
	}
}

func TestProjectStartShiftPreservesExplicitEditsAndNewTasks(t *testing.T) {
	old, cal, employees := startShiftFixture(t)
	p := copyStartProject(t, old)
	p.Start = "2026-10-30"
	p.Tasks[0].RequestedStart = "2026-11-09"
	for i := range p.Tasks {
		if p.Tasks[i].ID == "done" {
			p.Tasks[i].Start, p.Tasks[i].End = "2026-10-01", "2026-10-02"
		}
		if p.Tasks[i].ID == "customer" {
			// Editing only the end still explicitly changes the manual range.
			p.Tasks[i].End = "2026-10-29"
		}
	}
	p.Tasks = append(p.Tasks, Task{ID: "new", Title: "New", Status: "todo", Responsibility: "internal", RequestedStart: "2026-10-30", DurationDays: 1})
	if err := shiftProjectStart(old, &p); err != nil {
		t.Fatal(err)
	}
	if err := Calculate(&p, cal, employees, schedulingNow); err != nil {
		t.Fatal(err)
	}
	if got := startTask(t, p, "a"); got.RequestedStart != "2026-11-09" || got.Start != "2026-11-09" {
		t.Fatalf("explicit task anchor was shifted: %+v", got)
	}
	if got := startTask(t, p, "done"); got.Start != "2026-10-01" || got.End != "2026-10-02" {
		t.Fatalf("explicit completed range was shifted: %+v", got)
	}
	if got := startTask(t, p, "customer"); got.Start != "2026-10-23" || got.End != "2026-10-29" {
		t.Fatalf("explicit customer range was shifted during recalculation: %+v", got)
	}
	if got := startTask(t, p, "new"); got.RequestedStart != "2026-10-30" || got.Start != "2026-10-30" {
		t.Fatalf("new task was shifted a second time: %+v", got)
	}
}

func TestProjectStartShiftKeepsCompletedCalendarSpan(t *testing.T) {
	old, cal, employees := startShiftFixture(t)
	p := copyStartProject(t, old)
	p.Start = "2026-10-27" // +4: finished Oct20..21 becomes Saturday..Sunday.
	if err := shiftProjectStart(old, &p); err != nil {
		t.Fatal(err)
	}
	if err := Calculate(&p, cal, employees, schedulingNow); err != nil {
		t.Fatal(err)
	}
	got := startTask(t, p, "done")
	if got.Start != "2026-10-24" || got.End != "2026-10-25" {
		t.Fatalf("completed range must not be rescheduled over weekends: %+v", got)
	}
	for _, load := range p.Loads {
		if load.TaskID == "done" {
			t.Fatal("completed task was allocated again")
		}
	}
}
