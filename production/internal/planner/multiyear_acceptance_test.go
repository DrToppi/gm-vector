package planner

import (
	"reflect"
	"testing"
)

// These are synthetic calendars, not official holiday data. In this fixture
// Jan 1/2 and Monday Jan 5 are holidays; Saturday Jan 3 is a transferred workday.
func multiYearAcceptanceCalendar(t *testing.T) (Calendar, map[string]string) {
	t.Helper()
	previous, next := annualFixture(t, 2025), annualFixture(t, 2026)
	next.Days["2026-01-01"] = Day{}
	next.Days["2026-01-02"] = Day{}
	next.Days["2026-01-03"] = Day{Working: true}
	next.Days["2026-01-05"] = Day{}
	recalculateChecksum(t, &next)
	cal, err := combineCalendars(previous, map[int]Calendar{2025: previous, 2026: next})
	if err != nil {
		t.Fatal(err)
	}
	return cal, map[string]string{"2025": previous.ID, "2026": next.ID}
}

func assertMultiYearAcceptanceLoads(t *testing.T, got, want []Load, planned int) {
	t.Helper()
	// Compare the entire ledger, including duplicate/missing entries, without
	// imposing an unrelated ordering contract on independent task allocations.
	counts := func(loads []Load) map[Load]int {
		result := map[Load]int{}
		for _, load := range loads {
			result[load]++
		}
		return result
	}
	if !reflect.DeepEqual(counts(got), counts(want)) {
		t.Fatalf("daily allocation differs:\n got %+v\nwant %+v", got, want)
	}
	total := 0
	for _, load := range got {
		if load.Minutes <= 0 {
			t.Fatalf("non-positive allocation: %+v", load)
		}
		total += load.Minutes
	}
	if total != planned {
		t.Fatalf("allocated %d minutes, expected planned total %d", total, planned)
	}
}

func TestMultiYearAcceptanceDependencyModes(t *testing.T) {
	// The predecessor needs three 60-minute allocations (Dec 30/31, Jan 3),
	// but its five-day manual minimum ends Jan 7 in working mode and Jan 3 in
	// calendar mode. A delay of three then skips three days in the selected
	// mode, before moving to a project workday. Expected dates below are literal.
	cases := []struct {
		name, mode, link, predecessorEnd, successorStart, successorEnd string
		delay                                                          int
		successorDates                                                 []string
	}{
		{"working/together", "working", "together", "2026-01-07", "2025-12-30", "2026-01-03", 0, []string{"2025-12-30", "2025-12-31", "2026-01-03"}},
		{"calendar/together", "calendar", "together", "2026-01-03", "2025-12-30", "2026-01-03", 0, []string{"2025-12-30", "2025-12-31", "2026-01-03"}},
		{"working/after", "working", "after", "2026-01-07", "2026-01-08", "2026-01-12", 0, []string{"2026-01-08", "2026-01-09", "2026-01-12"}},
		{"calendar/after", "calendar", "after", "2026-01-03", "2026-01-06", "2026-01-08", 0, []string{"2026-01-06", "2026-01-07", "2026-01-08"}},
		{"working/delay", "working", "delay", "2026-01-07", "2026-01-13", "2026-01-15", 3, []string{"2026-01-13", "2026-01-14", "2026-01-15"}},
		{"calendar/delay", "calendar", "delay", "2026-01-03", "2026-01-07", "2026-01-09", 3, []string{"2026-01-07", "2026-01-08", "2026-01-09"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cal, pins := multiYearAcceptanceCalendar(t)
			p := Project{
				ID: "year-dependencies", Title: "Synthetic cross-year links", Start: "2025-12-30",
				Mode: tc.mode, CalendarID: pins["2025"], CalendarVersions: pins,
				Tasks: []Task{
					{ID: "a", Title: "Predecessor", Status: "doing", Responsibility: "internal", RequestedStart: "2025-12-30", Planned: 180, DurationDays: 5, Assignments: []Assignment{{EmployeeID: "e", Daily: 60}}},
					{ID: "b", Title: "Successor", Status: "todo", Responsibility: "internal", Planned: 180, Assignments: []Assignment{{EmployeeID: "f", Daily: 60}}},
				},
				Links: []Link{{From: "a", To: "b", Mode: tc.link, Days: tc.delay}},
				People: []Participation{
					{EmployeeID: "e", Week: testParticipationWeek([]int{1, 2, 3, 4, 5, 6}, 60, "09:00", "10:00")},
					{EmployeeID: "f", Week: testParticipationWeek([]int{1, 2, 3, 4, 5, 6}, 60, "11:00", "12:00")},
				},
			}
			employees := map[string]Employee{"e": {ID: "e", Daily: 60}, "f": {ID: "f", Daily: 60}}
			if err := Calculate(&p, cal, employees, multiYearNow); err != nil {
				t.Fatal(err)
			}
			if p.CalculationState != "ready" || len(p.ProvisionalYears) != 0 {
				t.Fatalf("verified calendars must calculate without provisional years: state=%s years=%v", p.CalculationState, p.ProvisionalYears)
			}
			if a := p.Tasks[0]; a.Start != "2025-12-30" || a.End != tc.predecessorEnd || a.DurationDays != 5 || a.Planned != 180 {
				t.Fatalf("predecessor/manual minimum differs: %+v; want end %s", a, tc.predecessorEnd)
			}
			if b := p.Tasks[1]; b.Start != tc.successorStart || b.End != tc.successorEnd || b.Planned != 180 {
				t.Fatalf("successor differs: %+v; want %s..%s", b, tc.successorStart, tc.successorEnd)
			}
			want := []Load{
				{EmployeeID: "e", TaskID: "a", Date: "2025-12-30", Minutes: 60},
				{EmployeeID: "e", TaskID: "a", Date: "2025-12-31", Minutes: 60},
				{EmployeeID: "e", TaskID: "a", Date: "2026-01-03", Minutes: 60},
			}
			for _, date := range tc.successorDates {
				want = append(want, Load{EmployeeID: "f", TaskID: "b", Date: date, Minutes: 60})
			}
			assertMultiYearAcceptanceLoads(t, p.Loads, want, 360)
		})
	}
}

func TestMultiYearAcceptanceIndividualAvailability(t *testing.T) {
	// Dec 31: e has 480 minutes but the shortened day caps it at 420; f has
	// a 60-minute date exception. Jan 2: e's normal 240-minute Friday is
	// replaced by 90 minutes, while f contributes 240. Transferred Jan 3:
	// e's exceptional Saturday contributes 120 and f's weekly Saturday 180.
	// Holiday Jan 1/5 stay empty even with active employee date exceptions.
	// Jan 6: e/f can contribute 120/300. All expected loads are listed here,
	// independently of the scheduler, including a final one-minute remainder.
	base := []Load{
		{EmployeeID: "e", TaskID: "a", Date: "2025-12-31", Minutes: 420},
		{EmployeeID: "f", TaskID: "a", Date: "2025-12-31", Minutes: 60},
		{EmployeeID: "e", TaskID: "a", Date: "2026-01-02", Minutes: 90},
		{EmployeeID: "f", TaskID: "a", Date: "2026-01-02", Minutes: 240},
		{EmployeeID: "e", TaskID: "a", Date: "2026-01-03", Minutes: 120},
		{EmployeeID: "f", TaskID: "a", Date: "2026-01-03", Minutes: 180},
	}
	cases := []struct {
		name    string
		planned int
		lastDay []Load
	}{
		{"full_day", 1530, []Load{{EmployeeID: "e", TaskID: "a", Date: "2026-01-06", Minutes: 120}, {EmployeeID: "f", TaskID: "a", Date: "2026-01-06", Minutes: 300}}},
		{"one_minute_remainder", 1111, []Load{{EmployeeID: "e", TaskID: "a", Date: "2026-01-06", Minutes: 1}}},
	}
	for _, mode := range []string{"working", "calendar"} {
		for _, tc := range cases {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				previous, next := annualFixture(t, 2025), annualFixture(t, 2026)
				previous.Days["2025-12-31"] = Day{Working: true, Shortened: true}
				next.Days["2026-01-01"] = Day{}
				next.Days["2026-01-03"] = Day{Working: true}
				next.Days["2026-01-05"] = Day{}
				recalculateChecksum(t, &previous)
				recalculateChecksum(t, &next)
				cal, err := combineCalendars(previous, map[int]Calendar{2025: previous, 2026: next})
				if err != nil {
					t.Fatal(err)
				}
				p := Project{
					ID: "year-availability", Title: "Synthetic individual availability", Start: "2025-12-31", Mode: mode,
					CalendarID: previous.ID, CalendarVersions: map[string]string{"2025": previous.ID, "2026": next.ID},
					Tasks: []Task{{ID: "a", Title: "Internal allocation", Status: "todo", Responsibility: "internal", RequestedStart: "2025-12-31", Planned: tc.planned, Assignments: []Assignment{{EmployeeID: "e", Daily: 480}, {EmployeeID: "f", Daily: 480}}}},
					People: []Participation{
						{EmployeeID: "e", Week: map[string]Activity{
							"1": {Active: true, Start: "09:00", End: "13:00", Daily: 240},
							"2": {Active: true, Start: "09:00", End: "11:00", Daily: 120},
							"3": {Active: true, Start: "09:00", End: "18:00", Daily: 480},
							"4": {Active: true, Start: "09:00", End: "13:00", Daily: 240},
							"5": {Active: true, Start: "10:00", End: "14:00", Daily: 240},
						}, Exceptions: map[string]Activity{
							"2026-01-02": {Active: true, Start: "13:00", End: "14:30", Daily: 90},
							"2026-01-03": {Active: true, Start: "15:00", End: "17:00", Daily: 120},
							"2026-01-05": {Active: true, Start: "14:00", End: "18:00", Daily: 240},
						}},
						{EmployeeID: "f", Week: map[string]Activity{
							"1": {Active: true, Start: "09:00", End: "13:00", Daily: 240},
							"2": {Active: true, Start: "09:00", End: "14:00", Daily: 300},
							"4": {Active: true, Start: "09:00", End: "13:00", Daily: 240},
							"5": {Active: true, Start: "08:00", End: "12:00", Daily: 240},
							"6": {Active: true, Start: "09:00", End: "12:00", Daily: 180},
						}, Exceptions: map[string]Activity{
							"2025-12-31": {Active: true, Start: "13:00", End: "14:00", Daily: 60},
							"2026-01-01": {Active: true, Start: "09:00", End: "13:00", Daily: 240},
						}},
					},
				}
				employees := map[string]Employee{"e": {ID: "e", Daily: 480}, "f": {ID: "f", Daily: 480}}
				if err = Calculate(&p, cal, employees, multiYearNow); err != nil {
					t.Fatal(err)
				}
				if p.CalculationState != "ready" || len(p.ProvisionalYears) != 0 || len(p.Warnings) != 0 {
					t.Fatalf("verified availability did not calculate cleanly: state=%s years=%v warnings=%v", p.CalculationState, p.ProvisionalYears, p.Warnings)
				}
				if task := p.Tasks[0]; task.Start != "2025-12-31" || task.End != "2026-01-06" || task.Planned != tc.planned {
					t.Fatalf("allocation span/planned effort differs: %+v", task)
				}
				want := append(append([]Load(nil), base...), tc.lastDay...)
				assertMultiYearAcceptanceLoads(t, p.Loads, want, tc.planned)
			})
		}
	}
}
