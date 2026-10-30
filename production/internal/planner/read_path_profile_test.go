package planner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type readPathPhaseKey struct{}
type readPathConnectionKey struct{}
type readPathRequestKey struct{}
type readPathRequestTiming struct{ capacityAcquired time.Time }
type readPathQueryStart struct {
	phase, sql string
	started    time.Time
}
type readPathSQLMetric struct {
	Calls, Rows, Errors int64
	Milliseconds        float64
}
type readPathTrace struct {
	mu      sync.Mutex
	metrics map[string]map[string]readPathSQLMetric
	locks   map[string][]float64
	barrier func(context.Context, string)
}

func (q *readPathTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	sql := strings.Join(strings.Fields(data.SQL), " ")
	if q.barrier != nil {
		ctx = context.WithValue(ctx, readPathConnectionKey{}, conn.PgConn().PID())
		q.barrier(ctx, sql)
	}
	phase, _ := ctx.Value(readPathPhaseKey{}).(string)
	return context.WithValue(ctx, readPathQueryStart{}, readPathQueryStart{phase, sql, time.Now()})
}
func (q *readPathTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	v, ok := ctx.Value(readPathQueryStart{}).(readPathQueryStart)
	if !ok || v.phase == "" {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.metrics == nil {
		q.metrics = map[string]map[string]readPathSQLMetric{}
	}
	if q.metrics[v.phase] == nil {
		q.metrics[v.phase] = map[string]readPathSQLMetric{}
	}
	m := q.metrics[v.phase][v.sql]
	m.Calls++
	m.Rows += data.CommandTag.RowsAffected()
	m.Milliseconds += float64(time.Since(v.started).Microseconds()) / 1000
	if data.Err != nil {
		m.Errors++
	}
	q.metrics[v.phase][v.sql] = m
	request, _ := ctx.Value(readPathRequestKey{}).(*readPathRequestTiming)
	if request != nil {
		if v.sql == "SELECT pg_advisory_xact_lock_shared(789504)" && data.Err == nil {
			request.capacityAcquired = time.Now()
		}
		if v.sql == "rollback" && !request.capacityAcquired.IsZero() {
			if q.locks == nil {
				q.locks = map[string][]float64{}
			}
			q.locks[v.phase] = append(q.locks[v.phase], float64(time.Since(request.capacityAcquired).Microseconds())/1000)
		}
	}
}
func (q *readPathTrace) takeLocks(phase string) []float64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	v := q.locks[phase]
	delete(q.locks, phase)
	return v
}
func (q *readPathTrace) take(phase string) map[string]readPathSQLMetric {
	q.mu.Lock()
	defer q.mu.Unlock()
	m := q.metrics[phase]
	delete(q.metrics, phase)
	return m
}

type readPathFixture struct {
	store    *Store
	app      *App
	trace    *readPathTrace
	projects []Project
	sessions []testSession
}

func newReadPathFixture(t *testing.T, count int) *readPathFixture {
	t.Helper()
	dsn := os.Getenv("GMA_TEST_DSN")
	if dsn == "" {
		t.Skip("set GMA_TEST_DSN for isolated read-path checks")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Path != "/gma_planner_test" {
		t.Fatal("read-path fixture refuses a non-test database")
	}
	ctx := context.Background()
	root, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schemaName := "readpath_" + newID()
	if _, err = root.DB.Exec(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		root.DB.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		root.DB.Exec(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
		root.DB.Close()
	})
	params := u.Query()
	params.Set("search_path", schemaName)
	u.RawQuery = params.Encode()
	cfg, err := pgxpool.ParseConfig(u.String())
	if err != nil {
		t.Fatal(err)
	}
	trace := &readPathTrace{}
	cfg.MaxConns = 12 // Match Open rather than silently changing production capacity.
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "10000"
	cfg.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	s := &Store{DB: pool}
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	a, err := NewApp(s, Config{Origin: "https://profile.invalid", Secret: random(32), WebDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	f := &readPathFixture{store: s, app: a, trace: trace}
	c26, c27 := annualFixture(t, 2026), annualFixture(t, 2027)
	c27.Days["2027-01-01"] = Day{}
	recalculateChecksum(t, &c27)
	for _, cal := range []Calendar{c26, c27} {
		if err = a.SeedCalendar(ctx, cal); err != nil {
			t.Fatal(err)
		}
	}
	calendar, err := combineCalendars(c26, map[int]Calendar{2026: c26, 2027: c27})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	employees := map[string]Employee{}
	for i := 0; i < 30; i++ {
		id := fmt.Sprintf("profile-employee-%02d", i)
		employees[id] = Employee{ID: id, Name: id, Daily: 480}
		if _, err = tx.Exec(ctx, "INSERT INTO employees(id,name,daily_minutes) VALUES($1,$1,480)", id); err != nil {
			t.Fatal(err)
		}
	}
	for n := 0; n < count; n++ {
		p := Project{ID: fmt.Sprintf("profile-project-%02d", n), Title: "Synthetic ready portfolio", Start: "2026-12-14", Mode: "working", CalendarID: c26.ID, CalendarVersions: map[string]string{"2027": c27.ID}, Tasks: []Task{}, Links: []Link{}, People: []Participation{}}
		if n%2 == 1 {
			p.CalendarID, p.CalendarVersions = c27.ID, map[string]string{"2026": c26.ID}
		}
		for i := 0; i < 30; i++ {
			id := fmt.Sprintf("profile-employee-%02d", i)
			p.People = append(p.People, Participation{EmployeeID: id, Week: testParticipationWeek([]int{1, 2, 3, 4, 5}, 96, "09:00", "18:00"), Exceptions: map[string]Activity{}})
		}
		for i := 0; i < 300; i++ {
			id := fmt.Sprintf("profile-task-%03d", i)
			p.Tasks = append(p.Tasks, Task{ID: id, Title: id, Status: "todo", Responsibility: "internal", RequestedStart: p.Start, Planned: 192, Assignments: []Assignment{{EmployeeID: fmt.Sprintf("profile-employee-%02d", i%30), Daily: 96}}})
			if i >= 30 {
				p.Links = append(p.Links, Link{From: fmt.Sprintf("profile-task-%03d", i-30), To: id, Mode: "after"})
			}
		}
		if err = Calculate(&p, calendar, employees, multiYearNow); err != nil {
			t.Fatal(err)
		}
		if p.CalculationState != "ready" || len(p.Loads) != 600 || p.Tasks[299].End != "2027-01-11" {
			t.Fatal("independent fixture oracle failed", p.CalculationState, len(p.Loads), p.Tasks[299].End)
		}
		pins, _ := json.Marshal(p.CalendarVersions)
		if _, err = tx.Exec(ctx, "INSERT INTO projects(id,title,start_date,duration_mode,calendar_id,calendar_versions) VALUES($1,$2,$3,$4,$5,$6)", p.ID, p.Title, p.Start, p.Mode, p.CalendarID, pins); err != nil {
			t.Fatal(err)
		}
		if err = writeProject(ctx, tx, p); err != nil {
			t.Fatal(err)
		}
		f.projects = append(f.projects, p)
	}
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("profile-user-%02d", i)
		session := testSession{cookie: token(), csrf: token()}
		if _, err = tx.Exec(ctx, "INSERT INTO users(id,username,name,password_hash,totp,active) VALUES($1,$1,$1,'synthetic-not-login',$2,true)", id, []byte("synthetic-not-login")); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, "INSERT INTO sessions(hash,user_id,csrf) VALUES($1,$2,$3)", hash(session.cookie), id, session.csrf); err != nil {
			t.Fatal(err)
		}
		for _, p := range f.projects {
			if _, err = tx.Exec(ctx, "INSERT INTO memberships VALUES($1,$2,'manager')", p.ID, id); err != nil {
				t.Fatal(err)
			}
		}
		f.sessions = append(f.sessions, session)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *readPathFixture) addFacts(t *testing.T) {
	t.Helper()
	for _, p := range f.projects {
		_, err := f.store.DB.Exec(context.Background(), `INSERT INTO time_entries(id,project_id,task_id,employee_id,work_date,minutes,actor)
		 SELECT $1||'-fact-'||n,$1,'profile-task-'||lpad(((n-1)/10)::text,3,'0'),'profile-employee-'||lpad((((n-1)/10)%30)::text,2,'0'),'2026-12-14',15,'profile-user-00' FROM generate_series(1,3000)n`, p.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
}
func (f *readPathFixture) digest(t *testing.T) string {
	t.Helper()
	h := sha256.New()
	for _, table := range []string{"projects", "tasks", "dependencies", "assignments", "participation", "time_entries", "calendars", "snapshots", "employees"} {
		rows, err := f.store.DB.Query(context.Background(), "SELECT row_to_json(t)::text FROM "+table+" t ORDER BY row_to_json(t)::text")
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var raw string
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			io.WriteString(h, table+"\x00"+raw+"\n")
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func readPathLog(t *testing.T, kind string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("READ_PATH_%s %s", kind, raw)
}

func TestReadPathProfile(t *testing.T) {
	if os.Getenv("GMA_READ_PATH_PROFILE") != "1" {
		t.Skip("opt-in synthetic read-path observations; not performance acceptance")
	}
	readPathLog(t, "ENVIRONMENT", map[string]any{"go": runtime.Version(), "cpus": runtime.NumCPU(), "gomaxprocs": runtime.GOMAXPROCS(0), "clock_utc": time.Now().UTC(), "seed": "ready-portfolio-v1", "latency_gate": "not approved", "transport": "loopback TLS; application auth/authorization enabled; nginx excluded"})
	for _, count := range []int{1, 5} {
		t.Run(fmt.Sprintf("projects_%d", count), func(t *testing.T) {
			f := newReadPathFixture(t, count)
			handler := f.app.Handler()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r = r.WithContext(context.WithValue(r.Context(), readPathPhaseKey{}, r.Header.Get("X-Synthetic-Profile-Phase")))
				r = r.WithContext(context.WithValue(r.Context(), readPathRequestKey{}, &readPathRequestTiming{}))
				handler.ServeHTTP(w, r)
			}))
			defer server.Close()
			server.Client().Timeout = 30 * time.Second
			for _, facts := range []int{0, 3000} {
				if facts != 0 {
					f.addFacts(t)
				}
				digest := f.digest(t)
				f.profilePhases(t, facts)
				for _, users := range []int{1, 5, 10} {
					f.profileHTTP(t, server, users, facts)
				}
				if after := f.digest(t); after != digest {
					t.Fatal("HTTP reads modified persisted project state", digest, after)
				}
				readPathLog(t, "PERSISTENCE", map[string]any{"projects": count, "facts_per_project": facts, "unchanged": true, "digest": digest, "excluded": "session heartbeat and diagnostic ring buffer"})
				f.profilePlans(t, facts)
			}
		})
	}
}

func (f *readPathFixture) profileHTTP(t *testing.T, server *httptest.Server, users, facts int) {
	t.Helper()
	phase := fmt.Sprintf("http-p%d-f%d-u%d", len(f.projects), facts, users)
	var cpuFile *os.File
	if path := os.Getenv("GMA_READ_PATH_CPU_PROFILE"); path != "" && len(f.projects) == 5 && facts == 3000 && users == 10 {
		var err error
		cpuFile, err = os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = pprof.StartCPUProfile(cpuFile); err != nil {
			cpuFile.Close()
			t.Fatal(err)
		}
		defer func() { pprof.StopCPUProfile(); cpuFile.Close() }()
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	poolBefore := f.store.DB.Stat()
	var mu sync.Mutex
	var samples []float64
	var errors []string
	var bytesRead int64
	var wg sync.WaitGroup
	start := time.Now()
	for user := 0; user < users; user++ {
		wg.Add(1)
		go func(user int) {
			defer wg.Done()
			for n := 0; n < 20; n++ {
				req, err := http.NewRequest("GET", server.URL+"/api/v1/projects/"+f.projects[(user+n)%len(f.projects)].ID, nil)
				if err != nil {
					panic(err)
				}
				req.Host = "profile.invalid"
				req.AddCookie(&http.Cookie{Name: "__Host-gma", Value: f.sessions[user].cookie})
				req.Header.Set("X-Synthetic-Profile-Phase", phase)
				started := time.Now()
				response, err := server.Client().Do(req)
				var body []byte
				if err == nil {
					body, err = io.ReadAll(response.Body)
					response.Body.Close()
				}
				elapsed := float64(time.Since(started).Microseconds()) / 1000
				if err == nil {
					var out struct{ Project Project }
					err = json.Unmarshal(body, &out)
					if err == nil && (response.StatusCode != 200 || out.Project.CalculationState != "ready" || len(out.Project.Tasks) != 300 || len(out.Project.Loads) != 600 || len(out.Project.ProvisionalYears) != 0 || out.Project.Tasks[299].End != "2027-01-11" || out.Project.Tasks[0].Actual != facts/20) {
						err = fmt.Errorf("status=%d state=%s tasks=%d loads=%d facts=%d", response.StatusCode, out.Project.CalculationState, len(out.Project.Tasks), len(out.Project.Loads), facts)
					}
				}
				mu.Lock()
				samples = append(samples, elapsed)
				bytesRead += int64(len(body))
				if err != nil {
					errors = append(errors, err.Error())
				}
				mu.Unlock()
			}
		}(user)
	}
	wg.Wait()
	elapsed := time.Since(start).Seconds()
	runtime.ReadMemStats(&after)
	poolAfter := f.store.DB.Stat()
	firstCompleted := samples[0]
	sort.Float64s(samples)
	percentile := func(percent int) float64 { return samples[(len(samples)*percent+99)/100-1] }
	metrics := f.trace.take(phase)
	lockDurations := f.trace.takeLocks(phase)
	sort.Float64s(lockDurations)
	lockMean, lockP95 := 0.0, 0.0
	for _, milliseconds := range lockDurations {
		lockMean += milliseconds
	}
	if len(lockDurations) > 0 {
		lockMean /= float64(len(lockDurations))
		lockP95 = lockDurations[(len(lockDurations)*95+99)/100-1]
	}
	var calls, rows int64
	for _, m := range metrics {
		calls, rows = calls+m.Calls, rows+m.Rows
	}
	readPathLog(t, "HTTP", map[string]any{"projects": len(f.projects), "facts_per_project": facts, "users": users, "samples": len(samples), "errors": errors, "seconds": elapsed, "requests_per_second": float64(len(samples)) / elapsed, "p50_ms": percentile(50), "p95_ms": percentile(95), "p99_ms": percentile(99), "max_ms": samples[len(samples)-1], "first_completed_ms": firstCompleted, "warmup": "component phase reads precede HTTP; first_completed is not a cold-read measurement", "sql_calls_per_request": float64(calls) / float64(len(samples)), "sql_rows_per_request": float64(rows) / float64(len(samples)), "response_bytes_per_request": bytesRead / int64(len(samples)), "client_server_process_alloc_bytes_per_request": (after.TotalAlloc - before.TotalAlloc) / uint64(len(samples)), "heap_inuse_bytes_after": after.HeapInuse, "pool_acquisition_total_ms": float64((poolAfter.AcquireDuration() - poolBefore.AcquireDuration()).Microseconds()) / 1000, "pool_empty_acquires": poolAfter.EmptyAcquireCount() - poolBefore.EmptyAcquireCount(), "capacity_lock_samples": len(lockDurations), "capacity_lock_held_mean_ms": lockMean, "capacity_lock_held_p95_ms": lockP95, "capacity_lock_endpoint": "shared lock acquisition completed to transaction rollback completed", "sql": metrics})
	if len(errors) != 0 {
		t.Fatal("synthetic reads failed", errors[0])
	}
}

func (f *readPathFixture) profilePhases(t *testing.T, facts int) {
	t.Helper()
	ctx := context.Background()
	tx, err := f.store.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = capacityLock(ctx, tx, true); err != nil {
		t.Fatal(err)
	}
	employees, err := readEmployees(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	var p Project
	var calendar Calendar
	for _, phase := range []string{"readProject", "calendar", "Calculate", "prepare", "JSON"} {
		phaseCtx := context.WithValue(ctx, readPathPhaseKey{}, phase)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		started := time.Now()
		for n := 0; n < 5; n++ {
			switch phase {
			case "readProject":
				p, err = readProject(phaseCtx, tx, f.projects[0].ID)
			case "calendar":
				calendar, err = readProjectCalendar(phaseCtx, tx, &p)
			case "Calculate":
				err = Calculate(&p, calendar, employees, multiYearNow)
			case "prepare":
				err = f.store.prepare(phaseCtx, tx, &p)
			case "JSON":
				_, err = json.Marshal(map[string]any{"project": p, "role": "manager"})
			}
			if err != nil {
				t.Fatal(phase, err)
			}
		}
		runtime.ReadMemStats(&after)
		readPathLog(t, "PHASE", map[string]any{"phase": phase, "projects": len(f.projects), "facts_per_project": facts, "samples": 5, "mean_ms": float64(time.Since(started).Microseconds()) / 5000, "process_alloc_bytes_per_operation": (after.TotalAlloc - before.TotalAlloc) / 5, "sql": f.trace.take(phase)})
	}
}

func (f *readPathFixture) profilePlans(t *testing.T, facts int) {
	t.Helper()
	employeeIDs := []string{}
	for i := 0; i < 30; i++ {
		employeeIDs = append(employeeIDs, fmt.Sprintf("profile-employee-%02d", i))
	}
	queries := []struct {
		label, sql string
		args       []any
	}{
		{"candidate_projects", "SELECT DISTINCT p.id FROM projects p JOIN assignments a ON a.project_id=p.id WHERE p.id<>$1 AND p.archived_at IS NULL AND a.employee_id=ANY($2::text[]) ORDER BY p.id", []any{f.projects[0].ID, employeeIDs}},
		{"time_aggregate", "SELECT task_id,sum(minutes) FROM time_entries WHERE project_id=$1 GROUP BY task_id", []any{f.projects[0].ID}},
		{"tasks", "SELECT * FROM tasks WHERE project_id=$1 ORDER BY sort_order,id", []any{f.projects[0].ID}},
		{"assignments", "SELECT task_id,employee_id,daily_minutes FROM assignments WHERE project_id=$1 ORDER BY employee_id", []any{f.projects[0].ID}},
	}
	for _, query := range queries {
		var plan json.RawMessage
		if err := f.store.DB.QueryRow(context.Background(), "EXPLAIN(ANALYZE,BUFFERS,FORMAT JSON) "+query.sql, query.args...).Scan(&plan); err != nil {
			t.Fatal(err)
		}
		readPathLog(t, "PLAN", map[string]any{"label": query.label, "projects": len(f.projects), "facts_per_project": facts, "plan": plan})
	}
}
