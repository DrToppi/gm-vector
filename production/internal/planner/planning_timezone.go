package planner

import (
	"context"
	"errors"
	"time"
	_ "time/tzdata" // Static packaged runtime cannot depend on host zone files.
)

const defaultPlanningTimezone = "Europe/Moscow"

type planningClockKey struct{}
type planningClock struct {
	now  time.Time
	zone string
}

func resolvePlanningTimezone(zone string) (string, *time.Location, error) {
	if zone == "" {
		zone = defaultPlanningTimezone
	}
	if zone == "Local" {
		return "", nil, errors.New("planning_timezone must be an explicit IANA location")
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return "", nil, errors.New("planning_timezone is unavailable in packaged timezone data")
	}
	return zone, loc, nil
}
func planningNow(ctx context.Context) (time.Time, error) {
	if clock, ok := ctx.Value(planningClockKey{}).(planningClock); ok {
		return clock.now, nil
	}
	_, loc, err := resolvePlanningTimezone("")
	if err != nil {
		return time.Time{}, err
	}
	return time.Now().In(loc), nil
}
func (a *App) planningTimezone() string {
	if a.Config.PlanningTimezone == "" {
		return defaultPlanningTimezone
	}
	return a.Config.PlanningTimezone
}

// Direct App entry points may have no HTTP context. Bind this App's immutable
// policy without mutating a Store shared by another App or losing the instant.
func (a *App) withPlanningClock(ctx context.Context) (context.Context, error) {
	zone := a.planningTimezone()
	clock, present := ctx.Value(planningClockKey{}).(planningClock)
	if present && clock.zone == zone {
		return ctx, nil
	}
	_, location, err := resolvePlanningTimezone(zone)
	if err != nil {
		return ctx, err
	}
	instant := time.Now()
	if present {
		instant = clock.now
	}
	return context.WithValue(ctx, planningClockKey{}, planningClock{now: instant.In(location), zone: zone}), nil
}
func (a *App) prepareProject(ctx context.Context, q DBTX, p *Project) error {
	ctx, err := a.withPlanningClock(ctx)
	if err != nil {
		return err
	}
	return a.Store.prepare(ctx, q, p)
}
func (a *App) normalizeMemberDates(ctx context.Context, q DBTX, role string, old Project, p *Project) error {
	ctx, err := a.withPlanningClock(ctx)
	if err != nil {
		return err
	}
	return a.Store.normalizeMemberDates(ctx, q, role, old, p)
}

// Nominal wall-clock windows must map one-to-one onto instants without an
// offset transition inside. Never let time.Date silently choose a gap/fold.
func validateWallWindow(day, start, end, zone string) error {
	_, loc, err := resolvePlanningTimezone(zone)
	if err != nil {
		return err
	}
	d, err := date(day)
	if err != nil {
		return err
	}
	minutes, err := windowMinutes(start, end)
	if err != nil {
		return err
	}
	clock, err := time.Parse("15:04", start)
	if err != nil {
		return err
	}
	wall := d.Add(time.Duration(clock.Hour()*60+clock.Minute()) * time.Minute)
	offsets := map[int]bool{}
	for h := -48; h <= 48; h++ {
		_, offset := d.Add(time.Duration(h) * time.Hour).In(loc).Zone()
		offsets[offset] = true
	}
	previous := 0
	for n := 0; n <= minutes; n++ {
		label := wall.Add(time.Duration(n) * time.Minute)
		matches, matchedOffset := 0, 0
		for offset := range offsets {
			candidate := label.Add(-time.Duration(offset) * time.Second).In(loc)
			if candidate.Year() == label.Year() && candidate.YearDay() == label.YearDay() && candidate.Hour() == label.Hour() && candidate.Minute() == label.Minute() && candidate.Second() == 0 {
				matches++
				matchedOffset = offset
			}
		}
		if matches != 1 || (n > 0 && matchedOffset != previous) {
			return errors.New("calendar window is nonexistent, ambiguous or crosses a timezone transition")
		}
		previous = matchedOffset
	}
	return nil
}
