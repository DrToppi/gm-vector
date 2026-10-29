package planner

import (
	"context"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestTargetWorkload(t *testing.T) {
	if os.Getenv("GMA_PERFORMANCE") != "1" {
		t.Skip("opt-in isolated workload")
	}
	dsn := os.Getenv("GMA_TEST_DSN")
	u, e := url.Parse(dsn)
	if e != nil || u.Path != "/gma_planner_test" {
		t.Fatal("test database only")
	}
	ctx := context.Background()
	root, e := Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer root.DB.Close()
	schema := "perf_" + newID()
	if _, e = root.DB.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer root.DB.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	s, e := Open(ctx, u.String())
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	a := &App{Store: s}
	_, cal, _ := fixture()
	if e = a.SeedCalendar(ctx, cal); e != nil {
		t.Fatal(e)
	}
	emps := map[string]Employee{}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("e%d", i)
		emps[id] = Employee{ID: id, Name: id, Daily: 480}
		if _, e = tx.Exec(ctx, "INSERT INTO employees(id,name,daily_minutes) VALUES($1,$1,480)", id); e != nil {
			t.Fatal(e)
		}
	}
	var first Project
	for n := 0; n < 20; n++ {
		p := Project{ID: fmt.Sprintf("p%d", n), Title: fmt.Sprintf("Synthetic %d", n), Start: "2026-10-01", CalendarID: cal.ID, Mode: "working", Tasks: []Task{}, Links: []Link{}, People: []Participation{}}
		for i := 0; i < 100; i++ {
			p.People = append(p.People, Participation{Week: map[string]Activity{"1": {Active: true, Start: "09:00", End: "18:00", Daily: 480}, "2": {Active: true, Start: "09:00", End: "18:00", Daily: 480}, "3": {Active: true, Start: "09:00", End: "18:00", Daily: 480}, "4": {Active: true, Start: "09:00", End: "18:00", Daily: 480}, "5": {Active: true, Start: "09:00", End: "18:00", Daily: 480}}, EmployeeID: fmt.Sprintf("e%d", i), Weekdays: []int{1, 2, 3, 4, 5}, WindowStart: "09:00", WindowEnd: "18:00", Exceptions: map[string]Activity{}})
		}
		for i := 0; i < 300; i++ {
			p.Tasks = append(p.Tasks, Task{ID: fmt.Sprintf("t%d", i), Title: fmt.Sprintf("Task %d", i), Status: "todo", Responsibility: "internal", RequestedStart: p.Start, Planned: 480, Assignments: []Assignment{{fmt.Sprintf("e%d", i%100), 60}}})
		}
		for gap := 1; len(p.Links) < 1500; gap++ {
			for i := 0; i+gap < 300 && len(p.Links) < 1500; i++ {
				p.Links = append(p.Links, Link{From: fmt.Sprintf("t%d", i), To: fmt.Sprintf("t%d", i+gap), Mode: "together"})
			}
		}
		if e = Calculate(&p, cal, emps, time.Now()); e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(ctx, "INSERT INTO projects(id,title,start_date,duration_mode,calendar_id) VALUES($1,$2,$3,$4,$5)", p.ID, p.Title, p.Start, p.Mode, p.CalendarID); e != nil {
			t.Fatal(e)
		}
		if e = writeProject(ctx, tx, p); e != nil {
			t.Fatal(e)
		}
		if n == 0 {
			first = p
		}
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	times := []float64{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("GET", "https://localhost/api/v1/projects/p0", nil)
			r.SetPathValue("id", "p0")
			for j := 0; j < 3; j++ {
				w := httptest.NewRecorder()
				start := time.Now()
				err := a.getProject(w, r, Principal{User: User{Admin: true}})
				elapsed := float64(time.Since(start).Microseconds()) / 1000
				if err != nil {
					t.Error(err)
				}
				mu.Lock()
				times = append(times, elapsed)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	sort.Float64s(times)
	t.Logf("20 projects x300 tasks x1500 links /100 employees; 10 concurrent reads p95=%.1fms", times[int(float64(len(times))*.95)])
	start := time.Now()
	tx, e = s.DB.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = writeProject(ctx, tx, first); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	t.Logf("300 task/1500 link save transaction: %s", time.Since(start))
	if times[int(float64(len(times))*.95)] > 500 {
		t.Error("read p95 target exceeded")
	}
}
