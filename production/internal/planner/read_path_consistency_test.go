package planner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func (f *readPathFixture) request(method, path string, body any, phase string) *http.Request {
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(method, f.app.Config.Origin+"/api/v1"+path, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", f.app.Config.Origin)
	r.Header.Set("X-CSRF-Token", f.sessions[0].csrf)
	r.AddCookie(&http.Cookie{Name: "__Host-gma", Value: f.sessions[0].cookie})
	return r.WithContext(context.WithValue(r.Context(), readPathPhaseKey{}, phase))
}

func readPathWaiting(ctx context.Context, store *Store, pid uint32) (bool, error) {
	var waiting bool
	err := store.DB.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock' AND query LIKE 'SELECT pg_advisory_xact_lock(789504)%')", pid).Scan(&waiting)
	return waiting, err
}

func TestReadPathRevisionFactsCoherence(t *testing.T) {
	t.Run("canceled_reader_releases_capacity", testReadPathCanceledReader)
	for _, operation := range []string{"add", "delete"} {
		for _, cancelWriter := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_cancel_%t", operation, cancelWriter), func(t *testing.T) {
				f := newReadPathFixture(t, 1)
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				path := "/projects/" + f.projects[0].ID
				entry := TimeEntry{TaskID: "profile-task-000", EmployeeID: "profile-employee-00", Date: "2026-12-14", Minutes: 15, Comment: "Synthetic controlled fact"}
				if operation == "delete" {
					response := httptest.NewRecorder()
					f.app.Handler().ServeHTTP(response, f.request("POST", path+"/time-entries", entry, "seed-fact"))
					if response.Code != 201 || json.Unmarshal(response.Body.Bytes(), &entry) != nil {
						t.Fatal("could not seed delete fixture", response.Code)
					}
				}
				initial, err := readProject(ctx, f.store.DB, f.projects[0].ID)
				if err != nil {
					t.Fatal(err)
				}
				arrived, release := make(chan struct{}), make(chan struct{})
				writerPID := make(chan uint32, 1)
				var arriveOnce, releaseOnce sync.Once
				unblock := func() { releaseOnce.Do(func() { close(release) }) }
				defer unblock()
				f.trace.barrier = func(queryCtx context.Context, sql string) {
					phase, _ := queryCtx.Value(readPathPhaseKey{}).(string)
					if phase == "held-facts-read" && strings.HasPrefix(sql, "SELECT task_id,sum(minutes) FROM time_entries") {
						arriveOnce.Do(func() { close(arrived) })
						select {
						case <-release:
						case <-queryCtx.Done():
						}
					}
					if phase == "facts-writer" && sql == "SELECT pg_advisory_xact_lock(789504)" {
						pid, _ := queryCtx.Value(readPathConnectionKey{}).(uint32)
						writerPID <- pid
					}
				}
				readResponse, readDone := httptest.NewRecorder(), make(chan struct{})
				go func() {
					defer close(readDone)
					r := f.request("GET", path, nil, "held-facts-read")
					f.app.Handler().ServeHTTP(readResponse, r.WithContext(context.WithValue(ctx, readPathPhaseKey{}, "held-facts-read")))
				}()
				defer func() {
					unblock()
					cancel()
					select {
					case <-readDone:
					case <-time.After(5 * time.Second):
						t.Error("controlled GET did not terminate during cleanup")
					}
				}()
				select {
				case <-arrived:
				case <-ctx.Done():
					t.Fatal("GET did not reach its controlled aggregate barrier")
				}
				writerCtx, stopWriter := context.WithCancel(ctx)
				defer stopWriter()
				method, suffix := "POST", "/time-entries"
				if operation == "delete" {
					method, suffix = "DELETE", suffix+"/"+entry.ID
				}
				writeResponse, writeDone := httptest.NewRecorder(), make(chan struct{})
				go func() {
					defer close(writeDone)
					r := f.request(method, path+suffix, entry, "facts-writer")
					f.app.Handler().ServeHTTP(writeResponse, r.WithContext(context.WithValue(writerCtx, readPathPhaseKey{}, "facts-writer")))
				}()
				defer func() {
					stopWriter()
					unblock()
					select {
					case <-writeDone:
					case <-time.After(5 * time.Second):
						t.Error("controlled fact writer did not terminate during cleanup")
					}
				}()
				var pid uint32
				select {
				case pid = <-writerPID:
				case <-writeDone:
					t.Fatal("fact writer completed without acquiring the portfolio lock")
				case <-ctx.Done():
					t.Fatal("fact writer did not reach the capacity lock")
				}
				for {
					waiting, err := readPathWaiting(ctx, f.store, pid)
					if err != nil {
						t.Fatal(err)
					}
					if waiting {
						break
					}
					select {
					case <-writeDone:
						t.Fatal("fact writer committed while the old input snapshot was held")
					case <-ctx.Done():
						t.Fatal("fact writer did not enter the observed database lock wait")
					default:
					}
				}
				if cancelWriter {
					stopWriter()
					select {
					case <-writeDone:
					case <-ctx.Done():
						t.Fatal("canceled writer did not stop")
					}
				}
				unblock()
				select {
				case <-readDone:
				case <-ctx.Done():
					t.Fatal("GET did not finish after release")
				}
				select {
				case <-writeDone:
				case <-ctx.Done():
					t.Fatal("fact writer did not finish after release")
				}
				var out struct{ Project Project }
				if err = json.Unmarshal(readResponse.Body.Bytes(), &out); err != nil || readResponse.Code != 200 || out.Project.Revision != initial.Revision || out.Project.Tasks[0].Actual != initial.Tasks[0].Actual {
					t.Fatal("read did not retain one coherent old revision/facts snapshot", err, readResponse.Code, out.Project.Revision, out.Project.Tasks[0].Actual)
				}
				after, err := readProject(ctx, f.store.DB, f.projects[0].ID)
				if err != nil {
					t.Fatal(err)
				}
				wantRevision, wantActual := initial.Revision, initial.Tasks[0].Actual
				if !cancelWriter {
					wantStatus, delta := 201, 15
					if operation == "delete" {
						wantStatus, delta = 200, -15
					}
					if writeResponse.Code != wantStatus {
						t.Fatal("serialized fact mutation failed", writeResponse.Code, writeResponse.Body.String())
					}
					wantRevision++
					wantActual += delta
				}
				if after.Revision != wantRevision || after.Tasks[0].Actual != wantActual {
					t.Fatal("fact mutation/cancellation changed authoritative data incorrectly", after.Revision, after.Tasks[0].Actual, wantRevision, wantActual)
				}
				readPathLog(t, "COHERENCE", map[string]any{"operation": operation, "canceled_writer": cancelWriter, "read_revision": out.Project.Revision, "read_actual_minutes": out.Project.Tasks[0].Actual, "stored_revision": after.Revision, "stored_actual_minutes": after.Tasks[0].Actual, "writer_wait_observed": true})
			})
		}
	}
}

func testReadPathCanceledReader(t *testing.T) {
	f := newReadPathFixture(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()
	arrived := make(chan struct{})
	var once sync.Once
	f.trace.barrier = func(queryCtx context.Context, sql string) {
		phase, _ := queryCtx.Value(readPathPhaseKey{}).(string)
		if phase == "cancel-reader" && strings.HasPrefix(sql, "SELECT task_id,sum(minutes) FROM time_entries") {
			once.Do(func() { close(arrived) })
			<-queryCtx.Done()
		}
	}
	response, readDone := httptest.NewRecorder(), make(chan struct{})
	go func() {
		defer close(readDone)
		r := f.request("GET", "/projects/"+f.projects[0].ID, nil, "cancel-reader")
		f.app.Handler().ServeHTTP(response, r.WithContext(context.WithValue(readCtx, readPathPhaseKey{}, "cancel-reader")))
	}()
	defer func() {
		cancelRead()
		select {
		case <-readDone:
		case <-time.After(5 * time.Second):
			t.Error("canceled reader did not terminate during cleanup")
		}
	}()
	select {
	case <-arrived:
	case <-ctx.Done():
		t.Fatal("reader did not reach controlled cancellation point")
	}
	writer, err := f.store.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback(context.Background())
	var pid uint32
	if err = writer.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- capacityLock(ctx, writer, false) }()
	for {
		waiting, err := readPathWaiting(ctx, f.store, pid)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err = <-done:
			t.Fatal("writer unexpectedly passed the held reader", err)
		case <-ctx.Done():
			t.Fatal("writer did not enter controlled lock wait")
		default:
		}
	}
	cancelRead()
	select {
	case <-readDone:
	case <-ctx.Done():
		t.Fatal("canceled reader did not stop")
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("canceled reader retained capacity")
	}
	if err = writer.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if response.Code == 200 || strings.Contains(response.Body.String(), `"project":`) || f.store.DB.Stat().AcquiredConns() != 0 {
		t.Fatal("canceled reader published a partial project or retained a pool connection", response.Code, f.store.DB.Stat().AcquiredConns())
	}
	readPathLog(t, "READER_CANCEL", map[string]any{"capacity_writer_recovered": true, "request_goroutine_joined": true, "pool_acquired_after": 0, "partial_project_published": false})
}

type readPathHeldWriter struct {
	*httptest.ResponseRecorder
	arrived chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *readPathHeldWriter) Write(body []byte) (int, error) {
	w.once.Do(func() { close(w.arrived); <-w.release })
	return w.ResponseRecorder.Write(body)
}

func TestReadPathSlowResponseReleasesCapacityLock(t *testing.T) {
	for _, mode := range []string{"GET", "preview", "export"} {
		t.Run(mode, func(t *testing.T) {
			f := newReadPathFixture(t, 1)
			w := &readPathHeldWriter{ResponseRecorder: httptest.NewRecorder(), arrived: make(chan struct{}), release: make(chan struct{})}
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(w.release) }) }
			defer unblock()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			getDone := make(chan struct{})
			go func() {
				defer close(getDone)
				method, path, body := "GET", "/projects/"+f.projects[0].ID, any(nil)
				if mode == "preview" {
					method, path, body = "POST", path+"/preview", saveRequest{Project: f.projects[0]}
				} else if mode == "export" {
					path += "/export"
				}
				r := f.request(method, path, body, "held-response")
				f.app.Handler().ServeHTTP(w, r.WithContext(context.WithValue(ctx, readPathPhaseKey{}, "held-response")))
			}()
			defer func() {
				unblock()
				cancel()
				select {
				case <-getDone:
				case <-time.After(5 * time.Second):
					t.Error("materialized response did not terminate during cleanup")
				}
			}()
			select {
			case <-w.arrived:
			case <-ctx.Done():
				t.Fatal("read did not materialize its JSON response")
			}
			writer, err := f.store.DB.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Rollback(context.Background())
			var pid uint32
			if err = writer.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
				t.Fatal(err)
			}
			writerDone := make(chan error, 1)
			go func() { writerDone <- capacityLock(ctx, writer, false) }()
		acquiring:
			for {
				select {
				case err = <-writerDone:
					if err != nil {
						t.Fatal(err)
					}
					break acquiring
				case <-ctx.Done():
					t.Fatal("writer could not acquire capacity while response was held")
				default:
				}
				waiting, err := readPathWaiting(ctx, f.store, pid)
				if err != nil || waiting {
					t.Fatal("materialized response retained the portfolio lock", err, waiting)
				}
			}
			if err = writer.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			unblock()
			select {
			case <-getDone:
			case <-ctx.Done():
				t.Fatal("released response did not complete")
			}
			if w.Code != 200 {
				t.Fatal("held materialized response failed", w.Code, w.Body.String())
			}
			readPathLog(t, "LOCK_RELEASE", map[string]any{"read": mode, "json_materialized": true, "capacity_writer_acquired_before_response_release": true})
		})
	}
}
