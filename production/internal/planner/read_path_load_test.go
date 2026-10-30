package planner

import (
	"bytes"
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
	"runtime/metrics"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const loadClients = 10

// Only bounded synthetic identifiers/counters are recorded. No request/response
// bodies, headers, session cookies, CSRF values or error messages are logged.
type readPathLoadSample struct {
	Client, Action, Project, Status, ResponseBytes int
	Endpoint, Outcome                              string
	Milliseconds                                   float64
	StartMS                                        int64
}
type readPathLoadResource struct {
	Seconds, RuntimeCPUEstimateSeconds, PoolAcquireMS         float64
	HeapAllocBytes, HeapInuseBytes, TotalAllocBytes, RSSBytes uint64
	RSSAvailable                                              bool
	Goroutines                                                int
	HTTPActive, PoolCanceledAcquires, PoolEmptyAcquires       int64
	PoolTotal, PoolAcquired, PoolMax                          int32
}
type readPathLoadState struct {
	mu             sync.Mutex
	accepted       map[string]int64
	acceptedBriefs map[string]map[string]bool
	markers        map[string]string
	serverStatuses map[string]map[int]int
	serverCanceled map[string]int
	serverWrites   map[string]int
	active         atomic.Int64
}
type readPathLoadWriter struct {
	http.ResponseWriter
	status int
}
type readPathLoadRoundTrip func(*http.Request) (*http.Response, error)

func (f readPathLoadRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type readPathLoadBrokenBody struct{}

func (readPathLoadBrokenBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (readPathLoadBrokenBody) Close() error             { return nil }

func TestReadPathLoadResponseClassification(t *testing.T) {
	// No database/listener: a controlled transport proves status/body failures
	// cannot be hidden behind cancellation or mislabelled as optimistic409.
	for _, tc := range []struct {
		name, code, outcome string
		status              int
		broken              bool
	}{
		{"revision_conflict", "revision_conflict", "optimistic_conflict", 409, false},
		{"other_conflict", "other_conflict", "unexpected_status", 409, false},
		{"rate_limit", "rate_limit", "unexpected_status", 429, false},
		{"server_error", "internal", "unexpected_status", 500, false},
		{"server_error_canceled_body", "internal", "unexpected_status_and_body_error", 500, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f := &readPathFixture{projects: []Project{{ID: "synthetic-project"}}, sessions: []testSession{{cookie: "synthetic-cookie", csrf: "synthetic-csrf"}}, app: &App{Config: Config{Origin: "https://profile.invalid"}}}
			client := &http.Client{Transport: readPathLoadRoundTrip(func(*http.Request) (*http.Response, error) {
				var body io.ReadCloser = io.NopCloser(strings.NewReader(fmt.Sprintf("{\"error\":{\"code\":%q}}", tc.code)))
				if tc.broken {
					cancel()
					body = readPathLoadBrokenBody{}
				}
				return &http.Response{StatusCode: tc.status, Body: body, Header: http.Header{}}, nil
			})}
			sample, _ := readPathLoadRequest(ctx, client, &httptest.Server{URL: "https://profile.invalid"}, f, 0, 0, 0, "save", "classification", Project{}, "", time.Now())
			if sample.Status != tc.status || sample.Outcome != tc.outcome {
				t.Fatalf("unexpected classification: status%d outcome%s", sample.Status, sample.Outcome)
			}
		})
	}
}

func (w *readPathLoadWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *readPathLoadWriter) Write(raw []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(raw)
}

func readPathLoadDurations(t *testing.T) (bool, time.Duration, time.Duration) {
	t.Helper()
	smoke := os.Getenv("GMA_READ_PATH_LOAD_SMOKE") == "1"
	values, reference := []int{600, 900}, []int{600, 900}
	for i, name := range []string{"GMA_READ_PATH_LOAD_MIXED_SECONDS", "GMA_READ_PATH_LOAD_SOAK_SECONDS"} {
		if smoke {
			values[i] = 20
		}
		if raw := os.Getenv(name); raw != "" {
			seconds, err := strconv.Atoi(raw)
			if err != nil || seconds < 20 || seconds > reference[i] {
				t.Fatalf("%s must be between20 and its reference duration", name)
			}
			if !smoke && seconds != reference[i] {
				t.Fatal("short durations require GMA_READ_PATH_LOAD_SMOKE=1; never acceptance")
			}
			values[i] = seconds
		}
	}
	return smoke, time.Duration(values[0]) * time.Second, time.Duration(values[1]) * time.Second
}

func readPathLoadProjectDigest(p Project) string {
	p.Revision, p.Brief = 0, "" // The only intentional persisted changes.
	raw, _ := json.Marshal(p)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func readPathLoadStableDigest(t *testing.T, f *readPathFixture) string {
	t.Helper()
	h := sha256.New()
	for _, table := range []string{"calendars", "employees", "time_entries", "memberships", "users"} {
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
			fmt.Fprintf(h, "%s\x00%s\n", table, raw)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}
func readPathLoadValidProject(p Project, id string) bool {
	if p.ID != id || p.CalculationState != "ready" || len(p.Tasks) != 300 || len(p.Loads) != 600 || len(p.ProvisionalYears) != 0 || p.Tasks[299].End != "2027-01-11" {
		return false
	}
	total := 0
	for _, task := range p.Tasks {
		if task.Planned != 192 || task.Actual != 150 || len(task.Assignments) != 1 || task.Assignments[0].Daily != 96 {
			return false
		}
	}
	for _, load := range p.Loads {
		if load.Minutes != 96 || load.Date == "2027-01-01" {
			return false
		}
		total += load.Minutes
	}
	return total == 57600
}

func readPathLoadRequest(ctx context.Context, client *http.Client, server *httptest.Server, f *readPathFixture, user, action, project int, endpoint, phase string, candidate Project, marker string, started time.Time) (readPathLoadSample, Project) {
	sample := readPathLoadSample{Client: user, Action: action, Project: project, Endpoint: endpoint, StartMS: time.Since(started).Milliseconds()}
	method, suffix := http.MethodGet, ""
	var body io.Reader
	if endpoint == "preview" || endpoint == "save" {
		method = http.MethodPost
		if endpoint == "save" {
			method = http.MethodPut
		} else {
			suffix = "/preview"
		}
		candidate.Brief = marker
		candidate.Loads, candidate.Warnings, candidate.ProvisionalYears, candidate.CalculationState = nil, nil, nil, ""
		raw, err := json.Marshal(saveRequest{Project: candidate})
		if err != nil {
			sample.Outcome = "request_error"
			return sample, Project{}
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, server.URL+"/api/v1/projects/"+f.projects[project].ID+suffix, body)
	if err != nil {
		sample.Outcome = "request_error"
		return sample, Project{}
	}
	req.Host = "profile.invalid"
	req.Header.Set("Origin", f.app.Config.Origin)
	req.Header.Set("X-CSRF-Token", f.sessions[user].csrf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Synthetic-Profile-Phase", phase)
	req.Header.Set("X-Synthetic-Load-Action", fmt.Sprintf("%s-%d-%d", phase, user, action))
	req.AddCookie(&http.Cookie{Name: "__Host-gma", Value: f.sessions[user].cookie})
	begin := time.Now()
	response, err := client.Do(req)
	var raw []byte
	if response != nil {
		sample.Status = response.StatusCode
		var readErr error
		raw, readErr = io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
		if err == nil {
			err = readErr
		}
		closeErr := response.Body.Close()
		if err == nil {
			err = closeErr
		}
	}
	sample.Milliseconds = float64(time.Since(begin).Microseconds()) / 1000
	sample.ResponseBytes = len(raw)
	if err != nil {
		sample.Outcome = "transport_error"
		if ctx.Err() != nil {
			sample.Outcome = "request_deadline_or_cancellation"
		}
		if sample.Status >= 400 {
			sample.Outcome = "unexpected_status_and_body_error"
		}
		return sample, Project{}
	}
	if sample.Status == http.StatusConflict && (endpoint == "preview" || endpoint == "save") {
		var problem struct {
			Error struct{ Code string }
		}
		sample.Outcome = "unexpected_status"
		if json.Unmarshal(raw, &problem) == nil && problem.Error.Code == "revision_conflict" {
			sample.Outcome = "optimistic_conflict"
		}
		return sample, Project{}
	}
	if sample.Status != http.StatusOK {
		sample.Outcome = "unexpected_status"
		return sample, Project{}
	}
	var p Project
	if endpoint == "preview" || endpoint == "save" {
		err = json.Unmarshal(raw, &p)
	} else {
		var envelope struct{ Project Project }
		err = json.Unmarshal(raw, &envelope)
		p = envelope.Project
	}
	if err != nil || len(raw) > 8<<20 {
		sample.Outcome = "decode_error"
	} else if !readPathLoadValidProject(p, f.projects[project].ID) {
		sample.Outcome = "invariant_error"
	} else {
		sample.Outcome = "ok"
	}
	return sample, p
}

func readPathLoadResourceSample(f *readPathFixture, state *readPathLoadState, started time.Time) readPathLoadResource {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	cpu := []metrics.Sample{{Name: "/cpu/classes/user:cpu-seconds"}, {Name: "/cpu/classes/gc/total:cpu-seconds"}, {Name: "/cpu/classes/scavenge/total:cpu-seconds"}}
	metrics.Read(cpu)
	totalCPU := 0.0
	for _, sample := range cpu {
		if sample.Value.Kind() == metrics.KindFloat64 {
			totalCPU += sample.Value.Float64()
		}
	}
	resource := readPathLoadResource{Seconds: time.Since(started).Seconds(), HeapAllocBytes: memory.HeapAlloc, HeapInuseBytes: memory.HeapInuse, TotalAllocBytes: memory.TotalAlloc, Goroutines: runtime.NumGoroutine(), RuntimeCPUEstimateSeconds: totalCPU, HTTPActive: state.active.Load()}
	if status, err := os.ReadFile("/proc/self/status"); err == nil {
		for _, line := range strings.Split(string(status), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 3 && fields[0] == "VmRSS:" && fields[2] == "kB" {
				kb, err := strconv.ParseUint(fields[1], 10, 64)
				resource.RSSBytes, resource.RSSAvailable = kb*1024, err == nil
			}
		}
	}
	pool := f.store.DB.Stat()
	resource.PoolTotal, resource.PoolAcquired, resource.PoolMax = pool.TotalConns(), pool.AcquiredConns(), pool.MaxConns()
	resource.PoolCanceledAcquires, resource.PoolEmptyAcquires = pool.CanceledAcquireCount(), pool.EmptyAcquireCount()
	resource.PoolAcquireMS = float64(pool.AcquireDuration().Microseconds()) / 1000
	return resource
}
func readPathLoadDistribution(samples []readPathLoadSample) map[string]any {
	values := make([]float64, 0, len(samples))
	statuses, outcomes := map[int]int{}, map[string]int{}
	var bytes int64
	for _, sample := range samples {
		values = append(values, sample.Milliseconds)
		statuses[sample.Status]++
		outcomes[sample.Outcome]++
		bytes += int64(sample.ResponseBytes)
	}
	sort.Float64s(values)
	result := map[string]any{"samples": len(samples), "statuses": statuses, "outcomes": outcomes, "response_bytes": bytes}
	if len(values) != 0 {
		for _, percentile := range []int{50, 95, 99} {
			result[fmt.Sprintf("p%d_ms", percentile)] = values[(len(values)*percentile+99)/100-1]
		}
		result["max_ms"] = values[len(values)-1]
	}
	return result
}

func readPathLoadPhase(t *testing.T, f *readPathFixture, server *httptest.Server, state *readPathLoadState, phase string, duration time.Duration, smoke bool) {
	t.Helper()
	beforeReads := f.digest(t)
	candidates := make([]Project, loadClients)
	auxiliary := make([]readPathLoadSample, 0, loadClients)
	preflight := time.Now()
	for user := 0; user < loadClients; user++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		sample, p := readPathLoadRequest(ctx, server.Client(), server, f, user, -1, user%5, "auxiliary_get", "preflight-"+phase, Project{}, "", preflight)
		cancel()
		auxiliary = append(auxiliary, sample)
		if sample.Outcome != "ok" {
			readPathLog(t, "LOAD_PREFLIGHT_ERROR", sample)
			t.Fatal("authenticated auxiliary GET failed; no mixed phase started")
		}
		candidates[user] = p
	}
	if f.digest(t) != beforeReads {
		t.Fatal("read-only preflight changed persisted project/fact/calendar state")
	}
	readPathLog(t, "LOAD_AUXILIARY", map[string]any{"phase": phase, "outside_logical_mix": true, "read_immutability": true, "distribution": readPathLoadDistribution(auxiliary)})
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	started := time.Now()
	var mu sync.Mutex
	var samples []readPathLoadSample
	resources := []readPathLoadResource{readPathLoadResourceSample(f, state, started)}
	samplingDone, sampled := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(sampled)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		ticks := 0
		for {
			select {
			case <-ticker.C:
				resource := readPathLoadResourceSample(f, state, started)
				resources = append(resources, resource)
				ticks++
				if ticks%6 == 0 {
					mu.Lock()
					completed := len(samples)
					mu.Unlock()
					readPathLog(t, "LOAD_PROGRESS", map[string]any{"phase": phase, "elapsed_seconds": resource.Seconds, "completed_actions": completed, "http_active": resource.HTTPActive, "pool_total": resource.PoolTotal, "pool_acquired": resource.PoolAcquired, "heap_inuse_bytes": resource.HeapInuseBytes, "goroutines": resource.Goroutines})
				}
			case <-samplingDone:
				return
			}
		}
	}()
	var clients sync.WaitGroup
	for user := 0; user < loadClients; user++ {
		clients.Add(1)
		go func(user int) {
			defer clients.Done()
			candidate := candidates[user]
			next := started.Add(time.Duration(user) * 50 * time.Millisecond)
			for action := 0; ; action++ {
				timer := time.NewTimer(time.Until(next))
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				if ctx.Err() != nil {
					return
				}
				// Complete20-action cycles are exactly18GET/1preview/1PUT.
				// Per-user offsets prevent a synchronized artificial write burst.
				endpoint := "get"
				switch (action + user*3) % 20 {
				case 18:
					endpoint = "preview"
				case 19:
					endpoint = "save"
				}
				marker := fmt.Sprintf("Synthetic load %s client%d action%d", phase, user, action)
				if endpoint == "save" {
					state.mu.Lock()
					state.markers[fmt.Sprintf("%s-%d-%d", phase, user, action)] = marker
					state.mu.Unlock()
				}
				actionStarted := time.Now()
				// Phase expiry stops new dispatch only. A started HTTP action
				// drains under its independent ordinary30-second request bound.
				requestCtx, finishRequest := context.WithTimeout(context.Background(), 30*time.Second)
				sample, p := readPathLoadRequest(requestCtx, server.Client(), server, f, user, action, user%5, endpoint, phase, candidate, marker, started)
				finishRequest()
				sample.StartMS = actionStarted.Sub(started).Milliseconds()
				mu.Lock()
				samples = append(samples, sample)
				mu.Unlock()
				if sample.Outcome == "ok" && endpoint != "preview" {
					candidate = p
				}
				// No retry/prefetch is hidden in an action after a conflict.
				next = actionStarted.Add(time.Second)
			}
		}(user)
	}
	clients.Wait()
	close(samplingDone)
	<-sampled
	resources = append(resources, readPathLoadResourceSample(f, state, started))
	elapsed := time.Since(started).Seconds()
	unexpected, canceled, conflicts := 0, 0, 0
	byEndpoint, successes := map[string][]readPathLoadSample{}, map[string][]readPathLoadSample{}
	perClient := make([]int, loadClients)
	lastStart, minimumGap := make([]int64, loadClients), make([]int64, loadClients)
	var anomalies []readPathLoadSample
	for _, sample := range samples {
		byEndpoint[sample.Endpoint] = append(byEndpoint[sample.Endpoint], sample)
		if perClient[sample.Client] > 0 {
			gap := sample.StartMS - lastStart[sample.Client]
			if minimumGap[sample.Client] == 0 || gap < minimumGap[sample.Client] {
				minimumGap[sample.Client] = gap
			}
			if gap < 1000 {
				t.Errorf("%s: client%d exceeded one logical action per second", phase, sample.Client)
			}
		}
		lastStart[sample.Client] = sample.StartMS
		perClient[sample.Client]++
		switch sample.Outcome {
		case "ok":
			successes[sample.Endpoint] = append(successes[sample.Endpoint], sample)
		case "optimistic_conflict":
			conflicts++
		case "request_deadline_or_cancellation":
			canceled++
			unexpected++
		default:
			unexpected++
		}
		threshold := 2000.0
		if sample.Endpoint == "get" {
			threshold = 500
		}
		if sample.Outcome != "ok" || sample.Milliseconds > threshold {
			anomalies = append(anomalies, sample)
		}
	}
	endpoints := map[string]any{}
	for _, endpoint := range []string{"get", "preview", "save"} {
		endpoints[endpoint] = map[string]any{"all": readPathLoadDistribution(byEndpoint[endpoint]), "successful": readPathLoadDistribution(successes[endpoint])}
	}
	locks := f.trace.takeLocks(phase)
	sort.Float64s(locks)
	lockP95, lockMax := 0.0, 0.0
	if len(locks) != 0 {
		lockP95, lockMax = locks[(len(locks)*95+99)/100-1], locks[len(locks)-1]
	}
	mode := "reference acceptance"
	if smoke {
		mode = "SHORT SMOKE ONLY; not SLO acceptance"
	}
	drain := elapsed - duration.Seconds()
	if drain < 0 {
		drain = 0
	}
	readPathLog(t, "LOAD_PHASE", map[string]any{"phase": phase, "mode": mode, "scheduling_seconds": duration.Seconds(), "actual_seconds_including_drain": elapsed, "drain_seconds": drain, "clients": loadClients, "logical_actions": len(samples), "logical_actions_per_second_including_drain": float64(len(samples)) / elapsed, "per_client_actions": perClient, "per_client_minimum_start_gap_ms": minimumGap, "target_mix_percent": map[string]int{"get": 90, "preview": 5, "save": 5}, "actual_logical_counts": map[string]int{"get": len(byEndpoint["get"]), "preview": len(byEndpoint["preview"]), "save": len(byEndpoint["save"])}, "auxiliary_gets_outside_mix": len(auxiliary), "unexpected_errors": unexpected, "optimistic409": conflicts, "request_timeouts_or_cancellations": canceled, "endpoints": endpoints, "all_slow_or_error_samples": anomalies, "sql": f.trace.take(phase), "capacity_shared_lock_samples": len(locks), "capacity_shared_lock_p95_ms": lockP95, "capacity_shared_lock_max_ms": lockMax, "lock_scope": "shared acquisition completed to rollback completed; exclusive acquisition wait in SQL timings", "resources_10s": resources, "pool_acquisition_delta_ms": resources[len(resources)-1].PoolAcquireMS - resources[0].PoolAcquireMS, "resource_scope": "combined client/server Go process; PostgreSQL/nginx excluded; runtime CPU is an estimate, RSS Linux-only; no host identity"})
	if unexpected != 0 {
		t.Errorf("%s: %d unexpected HTTP/transport/decode/invariant failures retained above", phase, unexpected)
	}
	for _, count := range perClient {
		if count == 0 {
			t.Errorf("%s: not all ten distinct clients executed actions", phase)
		}
	}
	minimum := loadClients
	if smoke {
		minimum = 1
	}
	for _, endpoint := range []string{"preview", "save"} {
		if len(successes[endpoint]) < minimum {
			t.Errorf("%s: insufficient successful %s operations (%d < %d); conflicts do not establish coverage", phase, endpoint, len(successes[endpoint]), minimum)
		}
	}
	for _, resource := range resources {
		if resource.PoolTotal > 12 || resource.PoolMax != 12 {
			t.Errorf("%s: pool exceeded the frozen12-connection limit", phase)
		}
	}
	if !smoke {
		for _, endpoint := range []string{"get", "preview", "save"} {
			limit := 2000.0
			if endpoint == "get" {
				limit = 500
			}
			for _, group := range [][]readPathLoadSample{byEndpoint[endpoint], successes[endpoint]} {
				if len(group) == 0 || readPathLoadDistribution(group)["p95_ms"].(float64) > limit {
					t.Errorf("%s/%s: p95 exceeds approved%.0fms (or no samples)", phase, endpoint, limit)
				}
			}
		}
	}
}

func TestReadPathLoad(t *testing.T) {
	if os.Getenv("GMA_READ_PATH_LOAD") != "1" {
		t.Skip("opt-in25-minute reference load; set GMA_READ_PATH_LOAD=1 (SMOKE is never acceptance)")
	}
	dsn, err := url.Parse(os.Getenv("GMA_TEST_DSN"))
	if err != nil || dsn.Path != "/gma_planner_test" || (dsn.Scheme != "postgres" && dsn.Scheme != "postgresql") {
		t.Fatal("explicit disposable gma_planner_test DSN required")
	}
	smoke, mixed, soak := readPathLoadDurations(t)
	if runtime.GOMAXPROCS(0) != 12 && !smoke {
		t.Fatal("reference acceptance requires frozen GOMAXPROCS12; use smoke for another environment")
	}
	f := newReadPathFixture(t, 5)
	f.addFacts(t)
	if len(f.sessions) != loadClients || f.store.DB.Stat().MaxConns() != 12 {
		t.Fatal("fixture requires ten distinct sessions and the existing12-connection pool")
	}
	state := &readPathLoadState{accepted: map[string]int64{}, acceptedBriefs: map[string]map[string]bool{}, markers: map[string]string{}, serverStatuses: map[string]map[int]int{}, serverCanceled: map[string]int{}, serverWrites: map[string]int{}}
	var handlers sync.WaitGroup
	handler := f.app.Handler()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlers.Add(1)
		defer handlers.Done()
		state.active.Add(1)
		defer state.active.Add(-1)
		phase := r.Header.Get("X-Synthetic-Profile-Phase")
		r = r.WithContext(context.WithValue(r.Context(), readPathPhaseKey{}, phase))
		r = r.WithContext(context.WithValue(r.Context(), readPathRequestKey{}, &readPathRequestTiming{}))
		response := &readPathLoadWriter{ResponseWriter: w}
		handler.ServeHTTP(response, r)
		state.mu.Lock()
		defer state.mu.Unlock()
		if state.serverStatuses[phase] == nil {
			state.serverStatuses[phase] = map[int]int{}
		}
		state.serverStatuses[phase][response.status]++
		if r.Context().Err() != nil {
			state.serverCanceled[phase]++
		}
		if r.Method == http.MethodPut && response.status == http.StatusOK {
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/projects/")
			state.accepted[id]++
			state.serverWrites[phase]++
			if state.acceptedBriefs[id] == nil {
				state.acceptedBriefs[id] = map[string]bool{}
			}
			state.acceptedBriefs[id][state.markers[r.Header.Get("X-Synthetic-Load-Action")]] = true
		}
	}))
	defer server.Close()
	transport := server.Client().Transport.(*http.Transport)
	transport.MaxConnsPerHost, transport.MaxIdleConnsPerHost, transport.MaxIdleConns = loadClients, loadClients, loadClients
	server.Client().Timeout = 30 * time.Second
	defer transport.CloseIdleConnections()
	stableBefore := readPathLoadStableDigest(t, f)
	baseline, revisions := map[string]string{}, map[string]int64{}
	for i, p := range f.projects {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		sample, current := readPathLoadRequest(ctx, server.Client(), server, f, i, -1, i, "verification_get", "baseline", Project{}, "", time.Now())
		cancel()
		if sample.Outcome != "ok" {
			readPathLog(t, "LOAD_BASELINE_ERROR", sample)
			t.Fatal("baseline materialized fixture invalid")
		}
		baseline[p.ID], revisions[p.ID] = readPathLoadProjectDigest(current), current.Revision
	}
	readPathLog(t, "LOAD_ENVIRONMENT", map[string]any{"smoke_not_acceptance": smoke, "go": runtime.Version(), "gomaxprocs": runtime.GOMAXPROCS(0), "cpus_available": runtime.NumCPU(), "seed": "ready-portfolio-v1/load-cycle-v1", "projects": 5, "tasks_per_project": 300, "shared_employees": 30, "facts_per_project": 3000, "distinct_sessions": loadClients, "mixed_seconds": mixed.Seconds(), "soak_seconds": soak.Seconds(), "client_max_logical_actions_per_second": 1, "client_stagger_ms": 50, "transport": "trusted loopback TLS; real auth/CSRF/roles; Host=profile.invalid; nginx/external network excluded", "latency_clock": "client.Do through full body read/close; candidate preparation and JSON decoding excluded", "auxiliary_policy": "10 reported GETs before each phase;5 baseline+5 final verification GETs outside logical mix", "coverage_floor": "10 successful previews/saves per full phase;1 each in smoke;409 separately", "no_auto_mutation_retries": true})
	for _, phase := range []struct {
		name     string
		duration time.Duration
	}{{"mixed", mixed}, {"soak", soak}} {
		readPathLoadPhase(t, f, server, state, phase.name, phase.duration, smoke)
	}
	handlers.Wait()
	finalPortfolio := map[string]int{}
	var totalWrites int64
	for i, p := range f.projects {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		sample, final := readPathLoadRequest(ctx, server.Client(), server, f, i, -1, i, "verification_get", "verification", Project{}, "", time.Now())
		cancel()
		if sample.Outcome != "ok" || readPathLoadProjectDigest(final) != baseline[p.ID] {
			t.Errorf("project%d final schedules/facts/settings differ from baseline", i)
		}
		state.mu.Lock()
		writes := state.accepted[p.ID]
		briefAccepted := state.acceptedBriefs[p.ID][final.Brief]
		state.mu.Unlock()
		totalWrites += writes
		if final.Revision != revisions[p.ID]+writes || writes > 0 && !briefAccepted {
			t.Errorf("project%d revision/brief differs from observed committed PUTs", i)
		}
		var snapshots, storedRevision int64
		if err := f.store.DB.QueryRow(context.Background(), "SELECT revision,(SELECT count(*) FROM snapshots WHERE project_id=$1) FROM projects WHERE id=$1", p.ID).Scan(&storedRevision, &snapshots); err != nil || storedRevision != final.Revision || snapshots != writes {
			t.Errorf("project%d revision/snapshot persistence mismatch", i)
		}
		for _, load := range final.Loads {
			key := load.EmployeeID + "/" + load.Date
			finalPortfolio[key] += load.Minutes
			if finalPortfolio[key] > 480 {
				t.Error("final portfolio exceeds global employee daily maximum")
			}
		}
	}
	if readPathLoadStableDigest(t, f) != stableBefore {
		t.Error("calendars/employees/facts/memberships/users changed during schedule-neutral load")
	}
	for table, want := range map[string]int{"projects": 5, "tasks": 1500, "dependencies": 1350, "assignments": 1500, "participation": 150, "employees": 30, "calendars": 2, "time_entries": 15000, "memberships": 50, "users": 10, "sessions": 10} {
		var count int
		if err := f.store.DB.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != want {
			t.Errorf("final %s row count differs from synthetic fixture", table)
		}
	}
	var auditWrites int64
	if err := f.store.DB.QueryRow(context.Background(), "SELECT count(*) FROM audit WHERE action='project.changed'").Scan(&auditWrites); err != nil || auditWrites != totalWrites {
		t.Error("final audit write count differs from observed committed PUTs")
	}
	handlers.Wait()
	state.mu.Lock()
	readPathLog(t, "LOAD_FINAL", map[string]any{"smoke_not_acceptance": smoke, "server_statuses": state.serverStatuses, "server_canceled_contexts": state.serverCanceled, "server_committed_puts": state.serverWrites, "committed_puts_total": totalWrites, "revision_snapshot_audit_counts_checked": true, "project_schedule_facts_normalized_digests_checked": true, "immutable_tables_digest_checked": true, "global_employee_daily_maximum_checked": true, "ambiguous_ack_policy": "server200 after commit determines persisted writes; lost replies remain unexpected failures and are never retried"})
	state.mu.Unlock()
	transport.CloseIdleConnections()
	runtime.GC()
	readPathLog(t, "LOAD_CLEANUP", readPathLoadResourceSample(f, state, time.Now()))
}
