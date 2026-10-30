package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

var multiYearNow = time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

func TestMultiYearAnnualBoundaryRegression(t *testing.T) {
	p, cal, employees := fixture()
	cal.allowProvisional = true
	cal.verifiedYears = map[int]bool{2026: true}
	p.Start = "2026-12-31"
	p.Mode = "calendar"
	p.Tasks[0].RequestedStart = p.Start
	p.Tasks[0].DurationDays = 3
	p.Tasks[0].Planned = 0
	p.Tasks[0].Assignments = nil
	p.Tasks[0].Responsibility = "customer"
	if err := Calculate(&p, cal, employees, multiYearNow); err != nil {
		t.Fatalf("year-boundary project should calculate through a provisional next year: %v", err)
	}
	if p.Tasks[0].End != "2027-01-04" {
		t.Fatalf("expected Monday after weekend, got %s", p.Tasks[0].End)
	}
}

func TestMultiYearDurationBeyond730Regression(t *testing.T) {
	p, cal, employees := fixture()
	p.Start = "2026-01-01"
	p.Mode = "calendar"
	p.Tasks[0].RequestedStart = p.Start
	p.Tasks[0].DurationDays = 731
	p.Tasks[0].Planned = 0
	p.Tasks[0].Assignments = nil
	p.Tasks[0].Responsibility = "customer"
	for d := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC); d.Year() <= 2028; d = d.AddDate(0, 0, 1) {
		cal.Days[fmtDate(d)] = Day{Working: d.Weekday() != time.Saturday && d.Weekday() != time.Sunday}
	}
	if err := Calculate(&p, cal, employees, multiYearNow); err != nil {
		t.Fatalf("731-day duration rejected: %v", err)
	}
	if p.Tasks[0].End != "2028-01-03" {
		t.Fatalf("731-day calendar span ended %s", p.Tasks[0].End)
	}
}

func annualFixture(t *testing.T, year int) Calendar {
	t.Helper()
	c := Calendar{ID: fmt.Sprintf("synthetic-%d-v1", year), Year: year, Title: "Synthetic annual fixture", Source: "synthetic fixture; not a production calendar", Days: map[string]Day{}}
	for d := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC); d.Year() == year; d = d.AddDate(0, 0, 1) {
		c.Days[fmtDate(d)] = Day{Working: d.Weekday() != time.Saturday && d.Weekday() != time.Sunday}
	}
	if err := validateCalendar(&c); err != nil {
		t.Fatal(err)
	}
	return c
}
func recalculateChecksum(t *testing.T, c *Calendar) {
	t.Helper()
	c.Checksum = ""
	if err := validateCalendar(c); err != nil {
		t.Fatal(err)
	}
}
func budgetError(t *testing.T, err error) {
	t.Helper()
	var ae appError
	if !errors.As(err, &ae) || ae.Status != 422 || ae.Code != "calculation_budget_exceeded" {
		t.Fatalf("expected budget error, got %v", err)
	}
}
func assertNoPartial(t *testing.T, p Project) {
	t.Helper()
	if len(p.Loads) != 0 || p.CalculationState == "ready" {
		t.Fatalf("partial ledger remained: %s %d", p.CalculationState, len(p.Loads))
	}
	for _, v := range p.Tasks {
		if v.Status != "done" && v.Responsibility != "customer" && (v.Start != "" || v.End != "") {
			t.Fatalf("partial dates remained for %s", v.ID)
		}
	}
}
func TestMultiYearVerifiedHolidaysDependenciesAndLeap(t *testing.T) {
	p, _, employees := fixture()
	p.Start = "2025-12-30"
	p.Mode = "working"
	p.People = nil
	c25, c26, c27 := annualFixture(t, 2025), annualFixture(t, 2026), annualFixture(t, 2027)
	c26.Days["2026-01-01"] = Day{}
	c26.Days["2026-01-02"] = Day{}
	c26.Days["2026-01-03"] = Day{Working: true}
	c27.Days["2027-01-01"] = Day{}
	recalculateChecksum(t, &c26)
	recalculateChecksum(t, &c27)
	cal, err := combineCalendars(c25, map[int]Calendar{2025: c25, 2026: c26, 2027: c27})
	if err != nil {
		t.Fatal(err)
	}
	p.Tasks = []Task{
		{ID: "a", Title: "Across first year", Status: "todo", Responsibility: "customer", RequestedStart: "2025-12-30", DurationDays: 3},
		{ID: "b", Title: "After first", Status: "todo", Responsibility: "customer", DurationDays: 2},
		{ID: "c", Title: "Across second year", Status: "todo", Responsibility: "customer", RequestedStart: "2026-12-31", DurationDays: 3},
	}
	p.Links = []Link{{From: "a", To: "b", Mode: "after"}, {From: "b", To: "c", Mode: "after"}}
	if err = Calculate(&p, cal, employees, multiYearNow); err != nil {
		t.Fatal(err)
	}
	want := [][2]string{{"2025-12-30", "2026-01-03"}, {"2026-01-05", "2026-01-06"}, {"2026-12-31", "2027-01-05"}}
	for i, v := range p.Tasks {
		if v.Start != want[i][0] || v.End != want[i][1] {
			t.Fatalf("%s: got %s..%s expected %v", v.ID, v.Start, v.End, want[i])
		}
	}
	if len(p.ProvisionalYears) != 0 {
		t.Fatal("verified schedule marked provisional", p.ProvisionalYears)
	}
	c28 := annualFixture(t, 2028)
	cal, err = combineCalendars(c28, map[int]Calendar{2028: c28})
	if err != nil {
		t.Fatal(err)
	}
	p.Start = "2028-02-28"
	p.Tasks = p.Tasks[:1]
	p.Tasks[0].RequestedStart = p.Start
	p.Tasks[0].DurationDays = 3
	p.Links = nil
	if err = Calculate(&p, cal, employees, multiYearNow); err != nil || p.Tasks[0].End != "2028-03-01" {
		t.Fatalf("leap date skipped: %s %v", p.Tasks[0].End, err)
	}
}
func TestMultiYearProvisionalVersionReplacementAndCorruption(t *testing.T) {
	p, _, employees := fixture()
	c26 := annualFixture(t, 2026)
	p.Start = "2026-12-31"
	p.Tasks[0].RequestedStart = p.Start
	p.Tasks[0].Planned = 480
	p.Tasks[0].Assignments[0].Daily = 240
	cal, err := combineCalendars(c26, map[int]Calendar{2026: c26})
	if err != nil {
		t.Fatal(err)
	}
	if err = Calculate(&p, cal, employees, multiYearNow); err != nil || p.Tasks[0].End != "2027-01-01" {
		t.Fatal(p.Tasks[0].End, err)
	}
	if !reflect.DeepEqual(p.ProvisionalYears, []int{2027}) {
		t.Fatal(p.ProvisionalYears)
	}
	if cal.Days["2027-01-01"].Working != true {
		t.Fatal("fallback day was not shared with daily-limit lookup")
	}
	c27 := annualFixture(t, 2027)
	c27.Days["2027-01-01"] = Day{}
	recalculateChecksum(t, &c27)
	pinned, err := combineCalendars(c26, map[int]Calendar{2026: c26, 2027: c27})
	if err != nil {
		t.Fatal(err)
	}
	if err = Calculate(&p, pinned, employees, multiYearNow); err != nil || p.Tasks[0].End != "2027-01-04" {
		t.Fatal(p.Tasks[0].End, err)
	}
	if len(p.ProvisionalYears) != 0 {
		t.Fatal("provisional warning retained after explicit verified version", p.ProvisionalYears)
	}
	delete(c27.Days, "2027-01-05")
	if _, err = combineCalendars(c26, map[int]Calendar{2026: c26, 2027: c27}); err == nil {
		t.Fatal("gap in pinned year inferred")
	}
	c27 = annualFixture(t, 2027)
	c27.Days["2027-01-05"] = Day{}
	if _, err = combineCalendars(c26, map[int]Calendar{2026: c26, 2027: c27}); err == nil {
		t.Fatal("changed checksum accepted")
	}
	if _, err = combineCalendars(c26, map[int]Calendar{2027: c26}); err == nil {
		t.Fatal("calendar/year mismatch accepted")
	}
}
func TestMultiYearEffortBeyond730Days(t *testing.T) {
	p, _, employees := fixture()
	p.Start = "2025-01-01"
	p.Tasks[0].RequestedStart = p.Start
	p.Tasks[0].Planned = 800
	p.Tasks[0].Assignments[0].Daily = 1
	calendars := map[int]Calendar{}
	for y := 2025; y <= 2028; y++ {
		calendars[y] = annualFixture(t, y)
	}
	cal, err := combineCalendars(calendars[2025], calendars)
	if err != nil {
		t.Fatal(err)
	}
	if err = Calculate(&p, cal, employees, multiYearNow); err != nil {
		t.Fatal(err)
	}
	if p.Tasks[0].End != "2028-01-25" || len(p.Loads) != 800 {
		t.Fatal(p.Tasks[0].End, len(p.Loads))
	}
}
func TestMultiYearCalculationBudgetAndLoadBound(t *testing.T) {
	for _, kind := range []string{"work", "loads", "bytes", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			p, cal, employees := fixture()
			b := newCalculationBudget(context.Background())
			switch kind {
			case "work":
				b.remaining = 2
			case "loads":
				b.maxLoads = 2
			case "bytes":
				b.maxBytes = 1
			case "cancel":
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				b.ctx = ctx
			}
			err := calculate(&p, cal, employees, multiYearNow, b)
			if kind == "cancel" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else {
				budgetError(t, err)
			}
			assertNoPartial(t, p)
		})
	}
	p, cal, employees := fixture()
	b := newCalculationBudget(context.Background())
	b.maxLoads = 3
	if err := calculate(&p, cal, employees, multiYearNow, b); err != nil || len(p.Loads) != 3 {
		t.Fatal(err, len(p.Loads))
	}
}
func TestMultiYearGraphExpansionUsesBudget(t *testing.T) {
	p, cal, employees := fixture()
	p.People = nil
	p.Tasks = []Task{{ID: "left", Title: "Left", Status: "todo", Responsibility: "internal"}, {ID: "right", Title: "Right", Status: "todo", Responsibility: "internal"}}
	for _, parent := range []string{"left", "right"} {
		for i := 0; i < 100; i++ {
			p.Tasks = append(p.Tasks, Task{ID: fmt.Sprintf("%s-%d", parent, i), Parent: parent, Title: "Leaf", Status: "todo", Responsibility: "internal"})
		}
	}
	p.Links = []Link{{From: "left", To: "right", Mode: "after"}}
	b := newCalculationBudget(context.Background())
	b.remaining = 500
	budgetError(t, calculate(&p, cal, employees, multiYearNow, b))
	assertNoPartial(t, p)
}
func denseMultiYearFixture(t *testing.T) (Project, Calendar, map[string]Employee) {
	t.Helper()
	anchor := annualFixture(t, 2025)
	cal, err := combineCalendars(anchor, map[int]Calendar{2025: anchor})
	if err != nil {
		t.Fatal(err)
	}
	p := Project{ID: "dense", Title: "Synthetic dense long-range plan", Start: "2025-01-01", Mode: "working", CalendarID: anchor.ID, Tasks: []Task{}, People: []Participation{}, Links: []Link{}}
	employees := map[string]Employee{}
	for e := 0; e < 30; e++ {
		id := fmt.Sprintf("employee-%02d", e)
		employees[id] = Employee{ID: id, Name: id, Daily: 480}
		p.People = append(p.People, Participation{EmployeeID: id, Week: testParticipationWeek([]int{1, 2, 3, 4, 5}, 480, "09:00", "18:00"), Exceptions: map[string]Activity{}})
		for task := 0; task < 10; task++ {
			start := "2025-01-01"
			if task >= 4 {
				start = "2026-01-01"
			}
			if task >= 7 {
				start = "2027-01-01"
			}
			p.Tasks = append(p.Tasks, Task{ID: fmt.Sprintf("task-%02d-%02d", e, task), Title: "100 working days", RequestedStart: start, Status: "todo", Responsibility: "internal", Planned: 12000, Assignments: []Assignment{{EmployeeID: id, Daily: 120}}})
		}
	}
	return p, cal, employees
}
func TestMultiYearDense300TasksAndLargeLegacyMetadata(t *testing.T) {
	p, cal, employees := denseMultiYearFixture(t)
	if err := Calculate(&p, cal, employees, multiYearNow); err != nil {
		t.Fatal(err)
	}
	if len(p.Loads) != 30000 {
		t.Fatal("dense schedule unexpectedly truncated", len(p.Loads))
	}
	totals := map[string]int{}
	for _, l := range p.Loads {
		totals[l.TaskID] += l.Minutes
	}
	for _, task := range p.Tasks {
		if totals[task.ID] != 12000 {
			t.Fatal("effort not conserved", task.ID, totals[task.ID])
		}
	}
	if !reflect.DeepEqual(p.ProvisionalYears, []int{2026, 2027}) {
		t.Fatal(p.ProvisionalYears)
	}
	p, cal, employees = fixture()
	p.Tasks = nil
	for i := 0; i < 225; i++ {
		p.Tasks = append(p.Tasks, Task{ID: fmt.Sprintf("legacy-%d", i), Title: "Legacy metadata", Description: strings.Repeat("x", 20000), Status: "todo", Responsibility: "internal"})
	}
	if err := Calculate(&p, cal, employees, multiYearNow); err != nil {
		t.Fatal("valid legacy metadata rejected", err)
	}
	raw, err := json.Marshal(p)
	if err != nil || len(raw) <= 4<<20 || len(raw) >= 5<<20 {
		t.Fatal("fixture must be between4MiB and5MiB", len(raw), err)
	}
	if len(snapshotProject(p).Loads) != 0 {
		t.Fatal("snapshot persisted derived ledger")
	}
}
func BenchmarkMultiYearDense300Tasks(b *testing.B) {
	// Build the same fixture without using testing.T fixture assertions in timing.
	for n := 0; n < b.N; n++ {
		b.StopTimer()
		p, c, employees := fixture()
		p.Start = "2025-01-01"
		p.Tasks = nil
		p.People = nil
		employees = map[string]Employee{}
		c.Days = map[string]Day{}
		c.allowProvisional = true
		c.verifiedYears = map[int]bool{}
		for e := 0; e < 30; e++ {
			id := fmt.Sprintf("e-%d", e)
			employees[id] = Employee{ID: id, Daily: 480}
			p.People = append(p.People, Participation{EmployeeID: id, Week: testParticipationWeek([]int{1, 2, 3, 4, 5}, 480, "09:00", "18:00")})
			for i := 0; i < 10; i++ {
				year := 2025
				if i >= 4 {
					year = 2026
				}
				if i >= 7 {
					year = 2027
				}
				p.Tasks = append(p.Tasks, Task{ID: fmt.Sprintf("t-%02d-%02d", e, i), Title: "Long plan", Status: "todo", Responsibility: "internal", RequestedStart: fmt.Sprintf("%d-01-01", year), Planned: 12000, Assignments: []Assignment{{id, 120}}})
			}
		}
		b.StartTimer()
		if err := Calculate(&p, c, employees, multiYearNow); err != nil {
			b.Fatal(err)
		}
	}
}

type cancelDuringBudget struct {
	context.Context
	remaining int
	done      chan struct{}
}

func (c *cancelDuringBudget) Done() <-chan struct{} { return c.done }
func (c *cancelDuringBudget) Err() error {
	if c.remaining > 0 {
		c.remaining--
		return nil
	}
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	return context.Canceled
}
func TestMultiYearCancellationDuringAllocation(t *testing.T) {
	p, cal, employees := fixture()
	p.Tasks[0].Planned = 10000
	ctx := &cancelDuringBudget{Context: context.Background(), remaining: 50, done: make(chan struct{})}
	b := newCalculationBudget(ctx)
	if err := calculate(&p, cal, employees, multiYearNow, b); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	assertNoPartial(t, p)
}

func TestMultiYearLateBudgetFailureRestoresCustomerInputDates(t *testing.T) {
	for _, existing := range []bool{false, true} {
		p, cal, employees := fixture()
		internal := p.Tasks[0]
		internal.ID = "z"
		internal.Planned = 10000
		customer := Task{ID: "a", Title: "Customer duration", Status: "todo", Responsibility: "customer", RequestedStart: p.Start, DurationDays: 3}
		if existing {
			customer.Start = "2026-10-19"
			customer.End = "2026-10-20"
		}
		p.Tasks = []Task{customer, internal}
		b := newCalculationBudget(context.Background())
		b.remaining = 20
		budgetError(t, calculate(&p, cal, employees, multiYearNow, b))
		assertNoPartial(t, p)
		if p.Tasks[0].Start != customer.Start || p.Tasks[0].End != customer.End {
			t.Fatalf("customer partial dates leaked: %v", p.Tasks[0])
		}
	}
}
