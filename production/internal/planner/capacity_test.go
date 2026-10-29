package planner

import (
	"reflect"
	"testing"
	"time"
)

func TestProjectQuotaSharedAcrossTasksDeterministically(t *testing.T) {
	p, cal, employees := fixture()
	p.People[0].Week = testParticipationWeek([]int{1, 2, 3, 4, 5}, 360, "09:00", "15:00")
	p.Tasks[0].Planned = 300
	p.Tasks[0].Assignments[0].Daily = 300
	other := p.Tasks[0]
	other.ID, other.Title = "b", "Second"
	p.Tasks = append(p.Tasks, other)
	original := copyStartProject(t, p)
	if err := Calculate(&p, cal, employees, schedulingNow); err != nil {
		t.Fatal(err)
	}
	want := []Load{
		{EmployeeID: "e", TaskID: "a", Date: "2026-10-23", Minutes: 300},
		{EmployeeID: "e", TaskID: "b", Date: "2026-10-23", Minutes: 60},
		{EmployeeID: "e", TaskID: "b", Date: "2026-10-26", Minutes: 240},
	}
	if p.CalculationState != "ready" || !reflect.DeepEqual(p.Loads, want) {
		t.Fatalf("tasks must consume one shared project pool: state=%s loads=%+v", p.CalculationState, p.Loads)
	}
	original.Tasks[0], original.Tasks[1] = original.Tasks[1], original.Tasks[0]
	if err := Calculate(&original, cal, employees, schedulingNow); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original.Loads, want) {
		t.Fatal("input task array order changed the deterministic allocation", original.Loads)
	}
}

func TestDailyWindowsAndDateExceptionsSetActualQuota(t *testing.T) {
	p, cal, employees := fixture()
	p.Start, p.Tasks[0].RequestedStart = "2026-10-19", "2026-10-19"
	p.Tasks[0].Assignments[0].Daily = 480
	p.People[0].Week = map[string]Activity{
		"1": {Active: true, Start: "09:00", End: "11:00", Daily: 120},
		"2": {Active: true, Start: "10:00", End: "14:00", Daily: 240},
		"3": {Active: true, Start: "09:00", End: "13:00", Daily: 240},
		"4": {Active: true, Start: "09:00", End: "14:00", Daily: 300},
		"5": {Active: true, Start: "09:00", End: "11:00", Daily: 120},
	}
	p.People[0].Exceptions = map[string]Activity{
		"2026-10-20": {Active: true, Start: "15:00", End: "16:30", Daily: 90},
		"2026-10-21": {Active: false},
	}
	if err := Calculate(&p, cal, employees, schedulingNow); err != nil {
		t.Fatal(err)
	}
	want := []Load{
		{EmployeeID: "e", TaskID: "a", Date: "2026-10-19", Minutes: 120},
		{EmployeeID: "e", TaskID: "a", Date: "2026-10-20", Minutes: 90},
		{EmployeeID: "e", TaskID: "a", Date: "2026-10-22", Minutes: 300},
		{EmployeeID: "e", TaskID: "a", Date: "2026-10-23", Minutes: 90},
	}
	if !reflect.DeepEqual(p.Loads, want) || p.Tasks[0].End != "2026-10-23" {
		t.Fatalf("per-day availability ignored: task=%+v loads=%+v", p.Tasks[0], p.Loads)
	}
	blocks, warnings := BuildBusyBlocks([]Project{p})
	if len(blocks) != 4 || len(warnings) != 0 {
		t.Fatal("valid daily blocks rejected", blocks, warnings)
	}
	for _, block := range blocks {
		if block.Date == "2026-10-20" && (block.Start != "15:00" || block.End != "16:30") {
			t.Fatalf("date-specific window was not used: %+v", block)
		}
	}
}

func TestTaskQuotaAndGlobalDailyLimitBoundContribution(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		assignment, quota, maximum int
		shortened                  bool
		want                       int
	}{
		{"assignment", 60, 240, 480, false, 60},
		{"project", 240, 45, 480, false, 45},
		{"employee", 240, 240, 100, false, 100},
		{"shortened", 540, 540, 480, true, 420},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, cal, employees := fixture()
			p.Tasks[0].Assignments[0].Daily = tc.assignment
			p.People[0].Week = testParticipationWeek([]int{1, 2, 3, 4, 5}, tc.quota, "09:00", "18:00")
			employees["e"] = Employee{ID: "e", Name: "Employee", Daily: tc.maximum}
			day := cal.Days["2026-10-23"]
			day.Shortened = tc.shortened
			cal.Days["2026-10-23"] = day
			if err := Calculate(&p, cal, employees, schedulingNow); err != nil {
				t.Fatal(err)
			}
			if len(p.Loads) == 0 || p.Loads[0].Minutes != tc.want {
				t.Fatalf("first day must contribute %d minutes: %+v", tc.want, p.Loads)
			}
			total := 0
			for _, load := range p.Loads {
				total += load.Minutes
			}
			if total != 600 {
				t.Fatal("limiting capacity changed total planned work", total)
			}
		})
	}
}

func TestManualMinimumExtendsAndShrinksWithCapacity(t *testing.T) {
	for _, mode := range []string{"working", "calendar"} {
		t.Run(mode, func(t *testing.T) {
			p, cal, employees := fixture()
			p.Mode = mode
			p.Tasks[0].Planned, p.Tasks[0].DurationDays = 960, 2
			p.Tasks[0].Assignments[0].Daily = 480
			p.People[0].Week = testParticipationWeek([]int{1, 2, 3, 4, 5}, 240, "09:00", "13:00")
			if err := Calculate(&p, cal, employees, schedulingNow); err != nil {
				t.Fatal(err)
			}
			if p.Tasks[0].End != "2026-10-28" || p.Tasks[0].DurationDays != 2 || p.Tasks[0].Planned != 960 {
				t.Fatalf("manual minimum must extend without replacing user input: %+v", p.Tasks[0])
			}
			p.People[0].Week = testParticipationWeek([]int{1, 2, 3, 4, 5}, 480, "09:00", "17:00")
			if err := Calculate(&p, cal, employees, schedulingNow); err != nil {
				t.Fatal(err)
			}
			if p.Tasks[0].End != "2026-10-26" || p.Tasks[0].DurationDays != 2 || p.Tasks[0].Planned != 960 {
				t.Fatalf("capacity improvement must allow shortening back: %+v", p.Tasks[0])
			}
		})
	}
}

func TestLegacyParticipationInvalidatesComputedScheduleOnly(t *testing.T) {
	p, cal, employees := startShiftFixture(t)
	done, customer := startTask(t, p, "done"), startTask(t, p, "customer")
	p.People[0].Week = nil // Persisted legacy data has no new project quota.
	if err := Calculate(&p, cal, employees, schedulingNow); err != nil {
		t.Fatal(err)
	}
	if p.CalculationState != "needs_participation" || len(p.Loads) != 0 || len(p.Warnings) == 0 {
		t.Fatal("legacy data must require configuration", p.CalculationState, p.Loads, p.Warnings)
	}
	if task := startTask(t, p, "a"); task.Start != "" || task.End != "" || task.Planned != 600 || task.Actual != 45 || task.Status != "doing" {
		t.Fatalf("stale internal dates/facts: %+v", task)
	}
	if !reflect.DeepEqual(startTask(t, p, "done"), done) || !reflect.DeepEqual(startTask(t, p, "customer"), customer) {
		t.Fatal("invalidation must preserve completed and customer ranges")
	}
	if summary := startTask(t, p, "group"); summary.Start != "" || summary.End != "" {
		t.Fatal("summary presents stale calculated dates", summary)
	}
	blocks, _ := BuildBusyBlocks([]Project{p})
	if len(blocks) != 0 {
		t.Fatal("incomplete schedule must not publish busy blocks")
	}
}

func TestInvalidDailyConfigurationRejected(t *testing.T) {
	for _, tc := range []struct {
		name string
		week map[string]Activity
	}{
		{"unknown weekday", map[string]Activity{"7": {}}},
		{"noncanonical weekday", map[string]Activity{"01": {}}},
		{"zero active quota", map[string]Activity{"1": {Active: true, Start: "09:00", End: "17:00"}}},
		{"quota exceeds window", map[string]Activity{"1": {Active: true, Start: "09:00", End: "10:00", Daily: 61}}},
		{"overnight window", map[string]Activity{"1": {Active: true, Start: "22:00", End: "02:00", Daily: 60}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, cal, employees := fixture()
			p.People[0].Week = tc.week
			if err := Calculate(&p, cal, employees, schedulingNow); err == nil {
				t.Fatal("invalid project participation was accepted")
			}
		})
	}
	p, cal, employees := fixture()
	p.People[0].Exceptions["2026-10-23"] = Activity{Active: true, Start: "09:00", End: "10:00", Daily: 61}
	if err := Calculate(&p, cal, employees, schedulingNow); err == nil {
		t.Fatal("invalid date exception quota was accepted")
	}
}

func TestNoTasksDoNotReserveQuota(t *testing.T) {
	p, cal, employees := fixture()
	p.Tasks = nil
	if err := Calculate(&p, cal, employees, schedulingNow); err != nil {
		t.Fatal(err)
	}
	if p.CalculationState != "ready" || len(p.Loads) != 0 {
		t.Fatal("participation without tasks must not reserve hours", p.CalculationState, p.Loads)
	}
}

func TestUnavailableScheduleDoesNotExposePartialAllocation(t *testing.T) {
	p, cal, employees := fixture()
	p.Tasks[0].Planned = 240
	other := p.Tasks[0]
	other.ID, other.Title = "b", "Unavailable"
	other.Assignments = []Assignment{{EmployeeID: "f", Daily: 240}}
	p.Tasks = append(p.Tasks, other)
	employees["f"] = Employee{ID: "f", Name: "Second", Daily: 480}
	p.People = append(p.People, Participation{EmployeeID: "f", Week: map[string]Activity{"1": {Active: false}}})
	if err := Calculate(&p, cal, employees, schedulingNow); err != nil {
		t.Fatal("valid unavailable configuration must save as a draft", err)
	}
	if p.CalculationState != "blocked_capacity" || len(p.Loads) != 0 {
		t.Fatal("failed scheduling leaked earlier partial allocation", p.CalculationState, p.Loads)
	}
	for _, task := range p.Tasks {
		if task.Start != "" || task.End != "" {
			t.Fatal("failed scheduling leaked computed dates", task)
		}
	}
}

func TestZeroCapacityAndExhaustedExceptionsAreDrafts(t *testing.T) {
	for _, kind := range []string{"zero maximum", "finite exception"} {
		t.Run(kind, func(t *testing.T) {
			p, cal, employees := fixture()
			if kind == "zero maximum" {
				employee := employees["e"]
				employee.Daily = 0
				employees["e"] = employee
			} else {
				p.People[0].Week = map[string]Activity{"1": {Active: false}}
				p.People[0].Exceptions = map[string]Activity{"2026-10-23": {Active: true, Start: "09:00", End: "13:00", Daily: 240}}
			}
			if err := Calculate(&p, cal, employees, schedulingNow); err != nil {
				t.Fatal("valid capacity exhaustion must not reject configuration", err)
			}
			if p.CalculationState != "blocked_capacity" || len(p.Loads) != 0 || p.Tasks[0].Start != "" || p.Tasks[0].End != "" {
				t.Fatal("unavailable draft exposed a partial schedule", p)
			}
		})
	}
}

func TestCapacityHorizonExhaustionIsDraft(t *testing.T) {
	p, cal, employees := fixture()
	start, _ := date(p.Start)
	for d := start; !d.After(start.AddDate(0, 0, 731)); d = d.AddDate(0, 0, 1) {
		cal.Days[fmtDate(d)] = Day{Working: d.Weekday() != time.Saturday && d.Weekday() != time.Sunday}
	}
	p.Tasks[0].Planned = 1000
	p.Tasks[0].Assignments[0].Daily = 1
	if err := Calculate(&p, cal, employees, schedulingNow); err != nil {
		t.Fatal("valid but insufficient horizon must remain a draft", err)
	}
	if p.CalculationState != "blocked_capacity" || len(p.Loads) != 0 || p.Tasks[0].Start != "" || p.Tasks[0].End != "" {
		t.Fatal("horizon exhaustion leaked a partial schedule", p)
	}
}

func TestRequiredMissingCalendarRemainsAnError(t *testing.T) {
	p, cal, employees := fixture()
	delete(cal.Days, "2026-10-26")
	if err := Calculate(&p, cal, employees, schedulingNow); err == nil {
		t.Fatal("missing calendar was swallowed as capacity exhaustion")
	}
	if p.CalculationState == "ready" || len(p.Loads) != 0 || p.Tasks[0].Start != "" || p.Tasks[0].End != "" {
		t.Fatal("calendar error leaked partial dates or loads", p)
	}
}
