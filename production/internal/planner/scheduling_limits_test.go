package planner

import (
	"reflect"
	"testing"
	"time"
)

// Use a fixed clock so readiness assertions do not change when CI runs later.
var schedulingNow = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

func TestActivityExceptionDoesNotOverrideProjectWeekend(t *testing.T) {
	for _, mode := range []string{"working", "calendar"} {
		t.Run(mode, func(t *testing.T) {
			p, cal, employees := fixture()
			p.Mode = mode
			p.Start = "2026-10-24" // Saturday, even though the employee is active.
			p.Tasks[0].RequestedStart = p.Start
			p.Tasks[0].Planned = 240
			p.People[0].Week = testParticipationWeek([]int{1}, 480, "09:00", "18:00")
			p.People[0].Exceptions["2026-10-24"] = Activity{Active: true, Start: "09:00", End: "13:00", Daily: 240}
			if err := Calculate(&p, cal, employees, schedulingNow); err != nil {
				t.Fatal(err)
			}
			want := []Load{{EmployeeID: "e", TaskID: "a", Date: "2026-10-26", Minutes: 240}}
			if p.Tasks[0].Start != "2026-10-26" || p.Tasks[0].End != "2026-10-26" || !reflect.DeepEqual(p.Loads, want) {
				t.Fatalf("activity must not create work on a project holiday: task=%+v loads=%+v", p.Tasks[0], p.Loads)
			}
		})
	}
}

func TestParallelAssignmentsUseEachEmployeesActivity(t *testing.T) {
	p, cal, employees := fixture()
	p.Start = "2026-10-19" // Monday.
	p.Tasks[0].RequestedStart = p.Start
	p.Tasks[0].Planned = 480
	p.People[0].Week = testParticipationWeek([]int{1}, 480, "09:00", "18:00")
	employees["f"] = Employee{ID: "f", Name: "Second", Daily: 480}
	p.People = append(p.People, Participation{EmployeeID: "f", Week: testParticipationWeek([]int{2}, 480, "09:00", "18:00")})
	p.Tasks[0].Assignments = append(p.Tasks[0].Assignments, Assignment{EmployeeID: "f", Daily: 240})
	if err := Calculate(&p, cal, employees, schedulingNow); err != nil {
		t.Fatal(err)
	}
	want := []Load{
		{EmployeeID: "e", TaskID: "a", Date: "2026-10-19", Minutes: 240},
		{EmployeeID: "f", TaskID: "a", Date: "2026-10-20", Minutes: 240},
	}
	if p.Tasks[0].End != "2026-10-20" || !reflect.DeepEqual(p.Loads, want) {
		t.Fatalf("an inactive assignee must contribute zero without stopping the other: task=%+v loads=%+v", p.Tasks[0], p.Loads)
	}
}

func TestDailyLimitBoundaryAndRecalculation(t *testing.T) {
	// Strict shared capacity supersedes the former warning-only policy.
	p, cal, employees := fixture()
	p.Tasks[0].Planned = 240
	second := p.Tasks[0]
	second.ID, second.Title = "b", "B"
	second.Assignments = []Assignment{{EmployeeID: "e", Daily: 240}}
	p.Tasks = append(p.Tasks, second)
	if err := Calculate(&p, cal, employees, schedulingNow); err != nil {
		t.Fatal(err)
	}
	if p.Tasks[1].End != "2026-10-23" {
		t.Fatal("exactly 480 minutes must fit one day", p.Tasks[1])
	}
	p.Tasks[1].Planned = 241
	p.Tasks[1].Assignments[0].Daily = 241
	if err := Calculate(&p, cal, employees, schedulingNow); err != nil {
		t.Fatal(err)
	}
	if p.Tasks[1].End != "2026-10-26" {
		t.Fatalf("one minute over the pool must move to next workday: %+v", p.Tasks[1])
	}
	if len(p.Warnings) != 0 {
		t.Fatalf("strict allocation must not generate overload warnings: %v", p.Warnings)
	}
	employee := employees["e"]
	employee.Daily = 481
	employees["e"] = employee
	p.People[0].Week = testParticipationWeek([]int{1, 2, 3, 4, 5}, 481, "09:00", "18:00")
	if err := Calculate(&p, cal, employees, schedulingNow); err != nil {
		t.Fatal(err)
	}
	if p.Tasks[1].End != "2026-10-23" {
		t.Fatal("increasing capacity must permit an earlier end", p.Tasks[1])
	}
}

func TestParticipationExceptionsStayWithinTheirProject(t *testing.T) {
	first, cal, employees := fixture()
	second, _, _ := fixture()
	second.ID = "other"
	first.Tasks[0].Planned = 240
	second.Tasks[0].Planned = 240
	first.People[0].Exceptions["2026-10-23"] = Activity{Active: false}
	if err := Calculate(&first, cal, employees, schedulingNow); err != nil {
		t.Fatal(err)
	}
	if err := Calculate(&second, cal, employees, schedulingNow); err != nil {
		t.Fatal(err)
	}
	if first.Tasks[0].Start != "2026-10-26" || second.Tasks[0].Start != "2026-10-23" {
		t.Fatalf("one employee has independent participation schedules: first=%+v second=%+v", first.Tasks[0], second.Tasks[0])
	}
	if employees["e"].Daily != 480 {
		t.Fatal("project participation must not alter the shared daily limit")
	}
}

func TestBusyWindowExceptionAndTouchingProjectBlocks(t *testing.T) {
	// Pure construction only: Teams stays disabled and no Graph call is made.
	p, _, _ := fixture()
	p.CalculationState = "ready"
	p.Loads = []Load{{EmployeeID: "e", TaskID: "a", Date: "2026-10-23", Minutes: 120}}
	p.People[0].Exceptions["2026-10-23"] = Activity{Active: true, Start: "13:00", End: "15:00", Daily: 120}
	other, _, _ := fixture()
	other.ID = "other"
	other.CalculationState = "ready"
	other.Loads = []Load{{EmployeeID: "e", TaskID: "b", Date: "2026-10-23", Minutes: 60}}
	other.People[0].Week = testParticipationWeek([]int{5}, 60, "15:00", "16:00")
	blocks, warnings := BuildBusyBlocks([]Project{p, other})
	if len(blocks) != 2 || len(warnings) != 0 {
		t.Fatalf("touching intervals must not be treated as overlapping: blocks=%+v warnings=%v", blocks, warnings)
	}
	for _, block := range blocks {
		if block.ProjectID == p.ID && (block.Start != "13:00" || block.End != "15:00") {
			t.Fatalf("date exception must replace the recurring window: %+v", block)
		}
	}
	other.People[0].Week = testParticipationWeek([]int{5}, 60, "14:59", "16:00")
	blocks, warnings = BuildBusyBlocks([]Project{p, other})
	if len(blocks) != 0 || len(warnings) == 0 {
		t.Fatalf("even a one-minute overlap must suppress conflicting publication: blocks=%+v warnings=%v", blocks, warnings)
	}
}

func TestDependencyReadinessUsesModeAndScheduledDate(t *testing.T) {
	cases := []struct {
		name, mode, predecessorStatus, today, wantStart string
		blocked                                         bool
	}{
		{"together in progress", "together", "doing", "2026-10-23", "2026-10-23", false},
		{"together not started", "together", "todo", "2026-10-23", "2026-10-23", true},
		{"after in progress", "after", "doing", "2026-10-26", "2026-10-26", true},
		{"after done before date", "after", "done", "2026-10-25", "2026-10-26", true},
		{"after done on date", "after", "done", "2026-10-26", "2026-10-26", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, cal, employees := fixture()
			p.Tasks[0].Planned = 240
			p.Tasks[0].Status = tc.predecessorStatus
			p.Tasks[0].Start, p.Tasks[0].End = "2026-10-23", "2026-10-23"
			successor := p.Tasks[0]
			successor.ID, successor.Title, successor.Status = "b", "B", "todo"
			successor.Start, successor.End = "", ""
			p.Tasks = append(p.Tasks, successor)
			p.Links = []Link{{From: "a", To: "b", Mode: tc.mode}}
			now, err := date(tc.today)
			if err != nil {
				t.Fatal(err)
			}
			if err := Calculate(&p, cal, employees, now); err != nil {
				t.Fatal(err)
			}
			got := p.Tasks[1]
			if got.Blocked != tc.blocked || got.Start != tc.wantStart {
				t.Fatalf("want start=%s blocked=%v, got %+v", tc.wantStart, tc.blocked, got)
			}
		})
	}
}
