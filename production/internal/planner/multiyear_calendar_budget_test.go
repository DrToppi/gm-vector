package planner

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5"
)

// This fixture deliberately ignores context, so cancellation must be checked by
// the calendar loader before it asks the database to read or decode anything.
type calendarBudgetDB struct {
	DBTX
	calendars map[string]Calendar
	queries   int
}

func (q *calendarBudgetDB) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	q.queries++
	return calendarBudgetRow{q.calendars[args[0].(string)]}
}

type calendarBudgetRow struct{ c Calendar }

func (r calendarBudgetRow) Scan(dest ...any) error {
	if r.c.ID == "" {
		return pgx.ErrNoRows
	}
	*dest[0].(*string), *dest[1].(*int) = r.c.ID, r.c.Year
	*dest[2].(*string), *dest[3].(*string), *dest[4].(*string) = r.c.Title, r.c.Source, r.c.Checksum
	raw, err := json.Marshal(r.c.Days)
	*dest[5].(*[]byte) = raw
	return err
}

func TestMultiYearCalendarLoaderCanceledBeforeSQL(t *testing.T) {
	anchor := annualFixture(t, 2026)
	q := &calendarBudgetDB{calendars: map[string]Calendar{anchor.ID: anchor}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := readProjectCalendar(ctx, q, &Project{CalendarID: anchor.ID})
	if !errors.Is(err, context.Canceled) || q.queries != 0 {
		t.Fatalf("canceled calendar loader performed %d SQL queries, returned %v", q.queries, err)
	}
}

func TestMultiYearPinnedCalendarValidationUsesSharedBudget(t *testing.T) {
	q := &calendarBudgetDB{calendars: map[string]Calendar{}}
	p := Project{CalendarVersions: map[string]string{}}
	calendars := map[int]Calendar{}
	for year := firstCalendarYear; year <= lastCalendarYear; year++ {
		c := annualFixture(t, year)
		q.calendars[c.ID], calendars[year] = c, c
		p.CalendarVersions[strconv.Itoa(year)] = c.ID
	}
	anchor := calendars[firstCalendarYear]
	p.CalendarID = anchor.ID
	budget := newCalculationBudget(context.Background())
	// Exactly enough for validating the pins and validating/merging the anchor,
	// not for reading any of the other 200 selected annual versions.
	budget.remaining = len(p.CalendarVersions) + 1 + 2*len(anchor.Days)
	result, err := readProjectCalendar(context.Background(), q, &p, budget)
	budgetError(t, err)
	if q.queries != 1 || len(result.Days) != 0 {
		t.Fatalf("budget exhausted after %d SQL queries; published %d partial dates", q.queries, len(result.Days))
	}
	budget = newCalculationBudget(context.Background())
	budget.remaining = 729 // Even the shortest annual validation + merge costs 730.
	result, err = combineCalendars(anchor, calendars, budget)
	budgetError(t, err)
	if budget.remaining != 729 || len(result.Days) != 0 {
		t.Fatal("annual validation/merge ran without reserving its complete budget")
	}
}

func TestMultiYearCalendarBudgetContinuesAcrossProjects(t *testing.T) {
	anchor := annualFixture(t, 2026)
	q := &calendarBudgetDB{calendars: map[string]Calendar{anchor.ID: anchor}}
	p := Project{CalendarID: anchor.ID}
	budget := newCalculationBudget(context.Background())
	budget.remaining = 731 // One pin check, 365 validations, 365 merged dates.
	if _, err := readProjectCalendar(context.Background(), q, &p, budget); err != nil {
		t.Fatal(err)
	}
	_, err := readProjectCalendar(context.Background(), q, &p, budget)
	budgetError(t, err)
	if q.queries != 1 || budget.remaining != 0 {
		t.Fatalf("shared budget reset between projects: queries=%d remaining=%d", q.queries, budget.remaining)
	}
}

func TestMultiYearCalendarLoaderCancellationBetweenYears(t *testing.T) {
	anchor, next := annualFixture(t, 2026), annualFixture(t, 2027)
	q := &calendarBudgetDB{calendars: map[string]Calendar{anchor.ID: anchor, next.ID: next}}
	p := Project{CalendarID: anchor.ID, CalendarVersions: map[string]string{"2027": next.ID}}
	ctx := &cancelDuringBudget{Context: context.Background(), remaining: 2, done: make(chan struct{})}
	_, err := readProjectCalendar(ctx, q, &p)
	if !errors.Is(err, context.Canceled) || q.queries != 1 {
		t.Fatalf("cancellation between years: SQL queries=%d, error=%v", q.queries, err)
	}
}
