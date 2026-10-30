package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

const (
	firstCalendarYear        = 2000
	lastCalendarYear         = 2200
	maxDurationDays          = 73414
	calculationMaxOperations = 2000000
	calculationMaxLoads      = 100000
	calculationMaxBytes      = 16 << 20
)

type calculationBudget struct {
	ctx       context.Context
	remaining int
	maxLoads  int
	maxBytes  int
}

func newCalculationBudget(ctx context.Context) *calculationBudget {
	return &calculationBudget{ctx: ctx, remaining: calculationMaxOperations, maxLoads: calculationMaxLoads, maxBytes: calculationMaxBytes}
}
func budgetExceeded() error {
	return appError{422, "calculation_budget_exceeded", "Calculation budget exceeded. Reduce the calculated workload or split the plan; no partial schedule was saved."}
}
func (b *calculationBudget) take(n int) error {
	if err := b.ctx.Err(); err != nil {
		return err
	}
	if n < 0 || n > b.remaining {
		return budgetExceeded()
	}
	b.remaining -= n
	return nil
}
func calculationFailure(err error) error {
	var typed appError
	if errors.As(err, &typed) {
		return typed
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return bad(err.Error())
}
func (c *Calendar) schedulingDay(d time.Time) (Day, bool, error) {
	if d.Year() < firstCalendarYear || d.Year() > lastCalendarYear {
		return Day{}, false, bad("Calculation date is outside supported calendar years 2000–2200")
	}
	key := fmtDate(d)
	value, ok := c.Days[key]
	provisional := c.allowProvisional && !c.verifiedYears[d.Year()]
	if !ok {
		if !provisional {
			return Day{}, false, fmt.Errorf("нет проверенного календаря на %s", key)
		}
		value = Day{Working: d.Weekday() != time.Saturday && d.Weekday() != time.Sunday}
		c.Days[key] = value
	}
	return value, provisional, nil
}
func validateCalendarVersions(p *Project) error {
	if len(p.CalendarVersions) > lastCalendarYear-firstCalendarYear+1 {
		return bad("Too many annual calendar versions")
	}
	for year, id := range p.CalendarVersions {
		n, err := strconv.Atoi(year)
		if err != nil || strconv.Itoa(n) != year || len(year) != 4 || n < firstCalendarYear || n > lastCalendarYear || !validID.MatchString(id) {
			return bad("Invalid annual calendar version")
		}
	}
	return nil
}
func readProjectCalendar(ctx context.Context, q DBTX, p *Project) (Calendar, error) {
	if err := validateCalendarVersions(p); err != nil {
		return Calendar{}, err
	}
	anchor, err := readCalendar(ctx, q, p.CalendarID)
	if err != nil {
		return Calendar{}, bad("Calendar anchor is unavailable")
	}
	calendars := map[int]Calendar{anchor.Year: anchor}
	for year, id := range p.CalendarVersions {
		n, _ := strconv.Atoi(year)
		if n == anchor.Year && id != anchor.ID {
			return Calendar{}, bad("Annual calendar conflicts with the anchor version")
		}
		if id == anchor.ID && n == anchor.Year {
			continue
		}
		c, err := readCalendar(ctx, q, id)
		if err != nil {
			return Calendar{}, bad("Selected annual calendar is unavailable")
		}
		if c.Year != n {
			return Calendar{}, bad("Annual calendar version does not match its year")
		}
		calendars[n] = c
	}
	return combineCalendars(anchor, calendars)
}
func combineCalendars(anchor Calendar, calendars map[int]Calendar) (Calendar, error) {
	result := anchor
	result.Days = map[string]Day{}
	result.allowProvisional = true
	result.verifiedYears = map[int]bool{}
	for year, c := range calendars {
		if c.Year != year {
			return Calendar{}, bad("Annual calendar version does not match its year")
		}
		// Persisted versions are revalidated; missing/altered verified data must not
		// become provisional working days merely because the schedule is long.
		if c.Checksum == "" {
			return Calendar{}, bad("Selected annual calendar has no checksum")
		}
		if err := validateCalendar(&c); err != nil {
			return Calendar{}, err
		}
		result.verifiedYears[year] = true
		for day, value := range c.Days {
			result.Days[day] = value
		}
	}
	return result, nil
}
func inheritCalendarVersions(ctx context.Context, q DBTX, old Project, next *Project) error {
	if next.CalendarVersions != nil {
		return nil
	}
	next.CalendarVersions = map[string]string{}
	for year, id := range old.CalendarVersions {
		next.CalendarVersions[year] = id
	}
	if next.CalendarID != old.CalendarID {
		// Only an omitted legacy field gets this compatibility normalization. An
		// explicitly submitted contradictory map is rejected by the loader.
		anchor, err := readCalendar(ctx, q, next.CalendarID)
		if err != nil {
			return bad("Calendar anchor is unavailable")
		}
		key := strconv.Itoa(anchor.Year)
		if next.CalendarVersions[key] == old.CalendarID {
			next.CalendarVersions[key] = next.CalendarID
		}
	}
	return nil
}
func checkProjectResultSize(p *Project, b *calculationBudget) error {
	raw, err := json.Marshal(struct {
		Loads            []Load   `json:"loads"`
		Warnings         []string `json:"warnings"`
		ProvisionalYears []int    `json:"provisional_years"`
	}{p.Loads, p.Warnings, p.ProvisionalYears})
	if err != nil {
		return err
	}
	if len(raw) > b.maxBytes {
		return budgetExceeded()
	}
	return nil
}

// Store only authoritative input and historical dates in new snapshots. Derived
// ledgers are recalculated after undo and can be much larger for multi-year plans.
func snapshotProject(p Project) Project {
	p.Loads = nil
	p.Warnings = nil
	p.ProvisionalYears = nil
	p.CalculationState = ""
	return p
}

func customerInputRanges(p *Project) map[string][2]string {
	out := map[string][2]string{}
	for _, t := range p.Tasks {
		if t.Responsibility == "customer" {
			out[t.ID] = [2]string{t.Start, t.End}
		}
	}
	return out
}
func restoreCustomerInputRanges(p *Project, ranges map[string][2]string) {
	for i := range p.Tasks {
		if r, ok := ranges[p.Tasks[i].ID]; ok {
			p.Tasks[i].Start, p.Tasks[i].End = r[0], r[1]
		}
	}
}
