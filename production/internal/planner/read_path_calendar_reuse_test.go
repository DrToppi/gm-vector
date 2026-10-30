package planner

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestReadPathAnnualReuseIsRequestScopedAndIsolated(t *testing.T) {
	c26, c27 := annualFixture(t, 2026), annualFixture(t, 2027)
	q := &calendarBudgetDB{calendars: map[string]Calendar{c26.ID: c26, c27.ID: c27}}
	ctx := context.Background()
	versions := map[string]Calendar{}
	budget := newCalculationBudget(ctx)
	firstProject := Project{CalendarID: c26.ID, CalendarVersions: map[string]string{"2027": c27.ID}}
	secondProject := Project{CalendarID: c27.ID, CalendarVersions: map[string]string{"2026": c26.ID}}
	first, err := readProjectCalendarReusing(ctx, q, &firstProject, budget, versions)
	if err != nil {
		t.Fatal(err)
	}
	second, err := readProjectCalendarReusing(ctx, q, &secondProject, budget, versions)
	if err != nil || q.queries != 2 || len(versions) != 2 {
		t.Fatal("same annual IDs were repeatedly loaded", err, q.queries, len(versions))
	}
	// Actual work is charged once for validation and once per project's copy.
	wantWork := 2*(len(c26.Days)+len(c27.Days)) + len(c26.Days) + len(c27.Days) + 6
	if budget.remaining != calculationMaxOperations-wantWork {
		t.Fatal("annual reuse bypassed real validation/merge work", budget.remaining, wantWork)
	}
	first.Days["2026-03-02"] = Day{}
	first.verifiedYears[2026] = false
	if !second.Days["2026-03-02"].Working || !second.verifiedYears[2026] || !versions[c26.ID].Days["2026-03-02"].Working {
		t.Fatal("project composites share mutable calendar data or provenance")
	}
	if _, provisional, err := first.schedulingDay(time.Date(2028, 1, 3, 0, 0, 0, 0, time.UTC)); err != nil || !provisional {
		t.Fatal("fixture provisional date", err, provisional)
	}
	if _, ok := second.Days["2028-01-03"]; ok {
		t.Fatal("a lazily inserted provisional date leaked into another project")
	}
	if _, ok := versions[c26.ID].Days["2028-01-03"]; ok {
		t.Fatal("provisional days were stored in the annual reuse map")
	}
	// Another immutable ID for the same year must remain a different version.
	variant := annualFixture(t, 2027)
	variant.ID = "synthetic-reuse-2027-variant"
	variant.Days["2027-01-04"] = Day{}
	recalculateChecksum(t, &variant)
	q.calendars[variant.ID] = variant
	variantProject := Project{CalendarID: variant.ID}
	changed, err := readProjectCalendarReusing(ctx, q, &variantProject, budget, versions)
	if err != nil || changed.Days["2027-01-04"].Working || !second.Days["2027-01-04"].Working || q.queries != 3 {
		t.Fatal("annual versions were keyed by year or shared mutable days", err, q.queries)
	}
	// A later prepare/request starts empty and revalidates database contents.
	corrupt := c26
	corrupt.Checksum = "altered"
	q.calendars[c26.ID] = corrupt
	if _, err := readProjectCalendarReusing(ctx, q, &firstProject, newCalculationBudget(ctx), map[string]Calendar{}); err == nil || q.queries != 4 {
		t.Fatal("annual validation leaked across prepare/request boundaries", err, q.queries)
	}
	if _, err := readProjectCalendar(ctx, q, &firstProject); err == nil || q.queries != 5 {
		t.Fatal("uncached direct loader skipped validation", err, q.queries)
	}
}

func TestReadPathAnnualReuseKeepsSelectionAndBudgetGuards(t *testing.T) {
	c26, c27 := annualFixture(t, 2026), annualFixture(t, 2027)
	q := &calendarBudgetDB{calendars: map[string]Calendar{c26.ID: c26, c27.ID: c27}}
	ctx := context.Background()
	versions := map[string]Calendar{}
	p := Project{CalendarID: c26.ID, CalendarVersions: map[string]string{"2027": c27.ID}}
	if _, err := readProjectCalendarReusing(ctx, q, &p, newCalculationBudget(ctx), versions); err != nil {
		t.Fatal(err)
	}
	for _, pins := range []map[string]string{{"2028": c27.ID}, {"2026": c27.ID}} {
		badProject := Project{CalendarID: c26.ID, CalendarVersions: pins}
		if _, err := readProjectCalendarReusing(ctx, q, &badProject, newCalculationBudget(ctx), versions); err == nil {
			t.Fatal("cached annual year mismatch or anchor conflict was ignored", pins)
		}
	}
	budget := newCalculationBudget(ctx)
	budget.remaining = 365 // Pin checking leaves too little for a 365-day cached copy.
	result, err := readProjectCalendarReusing(ctx, q, &p, budget, versions)
	budgetError(t, err)
	if len(result.Days) != 0 || q.queries != 2 {
		t.Fatal("failed cached merge published partial days or queried SQL", len(result.Days), q.queries)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := readProjectCalendarReusing(canceled, q, &p, newCalculationBudget(canceled), versions); !errors.Is(err, context.Canceled) || q.queries != 2 {
		t.Fatal("cached annual load ignored cancellation", err, q.queries)
	}
}
