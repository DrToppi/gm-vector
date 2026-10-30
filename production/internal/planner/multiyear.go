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
func calendarBudget(ctx context.Context, supplied []*calculationBudget) *calculationBudget {
	if len(supplied) > 0 && supplied[0] != nil {
		return supplied[0]
	}
	return newCalculationBudget(ctx)
}
func readProjectCalendar(ctx context.Context, q DBTX, p *Project, supplied ...*calculationBudget) (Calendar, error) {
	budget := calendarBudget(ctx, supplied)
	// Check cancellation before SQL and account for checking every annual pin.
	if err := budget.take(len(p.CalendarVersions) + 1); err != nil {
		return Calendar{}, err
	}
	if err := validateCalendarVersions(p); err != nil {
		return Calendar{}, err
	}
	anchor, err := readCalendar(ctx, q, p.CalendarID)
	if err != nil {
		if ctx.Err() != nil {
			return Calendar{}, ctx.Err()
		}
		return Calendar{}, bad("Calendar anchor is unavailable")
	}
	result := compositeCalendar(anchor)
	if err := mergeAnnualCalendar(&result, anchor.Year, anchor, budget); err != nil {
		return Calendar{}, err
	}
	for year, id := range p.CalendarVersions {
		// Load and validate one version at a time. Exhausting the shared budget
		// must stop before reading all other years or the next shared project.
		if err := budget.take(1); err != nil {
			return Calendar{}, err
		}
		n, _ := strconv.Atoi(year)
		if n == anchor.Year && id != anchor.ID {
			return Calendar{}, bad("Annual calendar conflicts with the anchor version")
		}
		if id == anchor.ID && n == anchor.Year {
			continue
		}
		c, err := readCalendar(ctx, q, id)
		if err != nil {
			if ctx.Err() != nil {
				return Calendar{}, ctx.Err()
			}
			return Calendar{}, bad("Selected annual calendar is unavailable")
		}
		if err := mergeAnnualCalendar(&result, n, c, budget); err != nil {
			return Calendar{}, err
		}
	}
	return result, nil
}
func compositeCalendar(anchor Calendar) Calendar {
	result := anchor
	result.Days = map[string]Day{}
	result.allowProvisional = true
	result.verifiedYears = map[int]bool{}
	return result
}
func mergeAnnualCalendar(result *Calendar, year int, c Calendar, budget *calculationBudget) error {
	if c.Year != year || year < firstCalendarYear || year > lastCalendarYear {
		return bad("Annual calendar version does not match its year")
	}
	start := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	validationDays := int(start.AddDate(1, 0, 0).Sub(start).Hours() / 24)
	// Reserve all per-day validation and merge operations before either loop.
	// A single annual version is bounded to 366 dates; check context each year.
	if err := budget.take(validationDays + len(c.Days)); err != nil {
		return err
	}
	// Persisted versions are revalidated; missing/altered verified data must not
	// become provisional working days merely because the schedule is long.
	if c.Checksum == "" {
		return bad("Selected annual calendar has no checksum")
	}
	if err := validateCalendar(&c); err != nil {
		return err
	}
	result.verifiedYears[year] = true
	for day, value := range c.Days {
		result.Days[day] = value
	}
	return nil
}
func combineCalendars(anchor Calendar, calendars map[int]Calendar, supplied ...*calculationBudget) (Calendar, error) {
	budget := calendarBudget(context.Background(), supplied)
	result := compositeCalendar(anchor)
	for year, c := range calendars {
		if err := mergeAnnualCalendar(&result, year, c, budget); err != nil {
			return Calendar{}, err
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
		previous, err := readCalendar(ctx, q, old.CalendarID)
		if err != nil {
			return bad("Previous calendar anchor is unavailable")
		}
		if previous.Year != anchor.Year {
			// The former anchor was an implicit verified annual selection. A
			// legacy client changing years cannot explicitly carry that pin.
			previousYear := strconv.Itoa(previous.Year)
			if _, selected := next.CalendarVersions[previousYear]; !selected {
				next.CalendarVersions[previousYear] = previous.ID
			}
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
