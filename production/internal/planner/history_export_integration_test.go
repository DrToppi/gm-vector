package planner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHistoryExportEncodingBoundaries(t *testing.T) {
	b := historyBuffer{max: 3}
	if err := b.append([]byte("ab")); err != nil {
		t.Fatal(err)
	}
	if err := b.append([]byte("c")); err != nil || b.String() != "abc" {
		t.Fatal("exact byte boundary", err)
	}
	var ae appError
	if err := b.append([]byte("d")); !errors.As(err, &ae) || ae.Status != 422 || ae.Code != "history_export_limit" || b.String() != "abc" {
		t.Fatal("overflow mutated encoded buffer", err)
	}
	comment := strings.Repeat("\x01", 4000) // Legal PostgreSQL text; each byte needs a six-byte JSON escape.
	raw, err := json.Marshal(HistoryItem{TimeEntry: TimeEntry{Comment: comment}})
	if err != nil || bytes.Count(raw, []byte(`\u0001`)) != 4000 {
		t.Fatal("legal control escaping", err)
	}
	var roundtrip HistoryItem
	if err = json.Unmarshal(raw, &roundtrip); err != nil || roundtrip.Comment != comment {
		t.Fatal("escaped text lost", err)
	}
	for _, delta := range []int{-1, 0} {
		out := historyBuffer{max: len(raw) + delta}
		err = out.append(raw)
		if delta < 0 && (err == nil || out.Len() != 0) {
			t.Fatal("expanded row bypassed budget")
		}
		if delta == 0 && (err != nil || !bytes.Equal(out.Bytes(), raw)) {
			t.Fatal("exact encoded row rejected", err)
		}
	}
}

func assertHistoryExportReleased(t *testing.T, f *readPathFixture) {
	t.Helper()
	if len(f.app.historyExportSlot) != 0 || f.store.DB.Stat().AcquiredConns() != 0 {
		t.Fatal("export retained slot or connection")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tx, err := f.store.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err = capacityLock(ctx, tx, false); err != nil {
		t.Fatal("export retained portfolio lock", err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestTimeHistoryExportIntegration(t *testing.T) {
	t.Run("materialization_blocks_actual_fact_writer", testHistoryExportCoherentMaterialization)
	t.Run("complete_filtered_and_unattributed", func(t *testing.T) {
		f := newReadPathFixture(t, 1)
		comment := "</script>,\"=SUM(1)\n\x01 Unicode Ω"
		seedHistoryRows(t, f, 10000, comment)
		h, path := f.app.Handler(), "/projects/"+f.projects[0].ID+"/time-history/export"
		w := historyCall(t, f, h, "GET", path, nil, 0)
		doc := historyDecode[HistoryExport](t, w, 200)
		if doc.Format != "gm-vector-time-history" || doc.SchemaVersion != 1 || doc.TotalCount != 10000 || doc.RecordedMinutes != 10000 || len(doc.Items) != 10000 {
			t.Fatal("incomplete export metadata")
		}
		if _, err := time.Parse(time.RFC3339Nano, doc.GeneratedAt); err != nil || !strings.HasSuffix(doc.GeneratedAt, "Z") {
			t.Fatal("export timestamp is not a UTC instant")
		}
		if doc.PlanningTimezone != defaultPlanningTimezone || doc.Revision != 0 || doc.Legacy.Minutes != 0 || doc.Order != historyOrder {
			t.Fatal("unexpected export context", doc.HistoryCommon)
		}
		if w.Header().Get("Content-Length") != strconv.Itoa(w.Body.Len()) || w.Header().Get("X-Accel-Buffering") != "no" {
			t.Fatal("missing complete length/streaming contract")
		}
		for i, entry := range doc.Items {
			if entry.ID != fmt.Sprintf("history-%06d", i+1) || entry.Comment != comment || entry.Date != "2024-02-29" || entry.Minutes != 1 {
				t.Fatal("export lost/reordered/changed facts", i)
			}
		}
		if last := doc.Items[9999]; last.EmployeeID != "" || last.EmployeeName != "" {
			t.Fatal("invented NULL employee")
		}
		var shape struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &shape); err != nil {
			t.Fatal(err)
		}
		for _, item := range shape.Items {
			if len(item) != 8 {
				t.Fatal("unexpected history fields")
			}
			for _, key := range []string{"email", "actor", "password_hash", "totp", "provider"} {
				if _, ok := item[key]; ok {
					t.Fatal("unexpected private field", key)
				}
			}
		}
		filtered := historyDecode[HistoryExport](t, historyCall(t, f, h, "GET", path+"?employee_id=profile-employee-00&date_from=2024-02-29&date_to=2024-02-29", nil, 0), 200)
		if filtered.TotalCount != 9999 || filtered.RecordedMinutes != 9999 || len(filtered.Items) != 9999 {
			t.Fatal("filter ignored or page-limited export")
		}
		assertHistoryExportReleased(t, f)
	})
	t.Run("actual_row_limit_and_exact_boundary", func(t *testing.T) {
		f := newReadPathFixture(t, 1)
		seedHistoryRows(t, f, 100001, "")
		h, path := f.app.Handler(), "/projects/"+f.projects[0].ID+"/time-history/export"
		w := historyCall(t, f, h, "GET", path, nil, 0)
		historyErrorCode(t, w, 422, "history_export_limit")
		if strings.Contains(w.Body.String(), `"items"`) || w.Header().Get("Content-Disposition") != "" {
			t.Fatal("row overflow published a partial file")
		}
		assertHistoryExportReleased(t, f)
		if _, err := f.store.DB.Exec(context.Background(), "DELETE FROM time_entries WHERE project_id=$1 AND id='history-100001'", f.projects[0].ID); err != nil {
			t.Fatal(err)
		}
		out := historyDecode[HistoryExport](t, historyCall(t, f, h, "GET", path, nil, 0), 200)
		if out.TotalCount != 100000 || out.RecordedMinutes != 100000 || len(out.Items) != 100000 || out.Items[0].ID != "history-000001" || out.Items[99999].ID != "history-100000" {
			t.Fatal("exact row boundary truncated")
		}
		assertHistoryExportReleased(t, f)
	})
	t.Run("actual_escaped_byte_budget", func(t *testing.T) {
		f := newReadPathFixture(t, 1)
		seedHistoryRows(t, f, 10000, strings.Repeat("\x01", 4000))
		h, path := f.app.Handler(), "/projects/"+f.projects[0].ID+"/time-history/export"
		w := historyCall(t, f, h, "GET", path, nil, 0)
		historyErrorCode(t, w, 422, "history_export_limit")
		if strings.Contains(w.Body.String(), `"items"`) || w.Header().Get("Content-Disposition") != "" || w.Body.Len() > 4096 {
			t.Fatal("byte overflow published partial history")
		}
		assertHistoryExportReleased(t, f)
		empty := historyDecode[HistoryExport](t, historyCall(t, f, h, "GET", path+"?date_from=2030-01-01", nil, 0), 200)
		if empty.TotalCount != 0 || len(empty.Items) != 0 {
			t.Fatal("narrow export did not recover after budget failure")
		}
	})
	t.Run("held_write_keeps_slot_releases_database_and_freezes_document", func(t *testing.T) {
		f := newReadPathFixture(t, 1)
		seedHistoryRows(t, f, 5, "Before the writer")
		h, path := f.app.Handler(), "/projects/"+f.projects[0].ID+"/time-history/export"
		w := &readPathHeldWriter{ResponseRecorder: httptest.NewRecorder(), arrived: make(chan struct{}), release: make(chan struct{})}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		done := make(chan struct{})
		var once sync.Once
		release := func() { once.Do(func() { close(w.release) }) }
		go func() {
			defer close(done)
			h.ServeHTTP(w, f.request("GET", path, nil, "history-held-write").WithContext(ctx))
		}()
		defer func() {
			release()
			cancel()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("held export did not join")
			}
		}()
		select {
		case <-w.arrived:
		case <-ctx.Done():
			t.Fatal("export never materialized")
		}
		if len(f.app.historyExportSlot) != 1 {
			t.Fatal("slot released before held write")
		}
		tx, err := f.store.DB.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(context.Background())
		writeCtx, stopWrite := context.WithTimeout(ctx, 2*time.Second)
		defer stopWrite()
		if err = capacityLock(writeCtx, tx, false); err != nil {
			t.Fatal("network output retained the database lock", err)
		}
		if _, err = tx.Exec(ctx, "INSERT INTO time_entries(id,project_id,task_id,work_date,minutes,comment) VALUES('new-after-export',$1,'profile-task-000','2024-02-29',3,'After the snapshot')", f.projects[0].ID); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, "UPDATE projects SET revision=revision+1 WHERE id=$1", f.projects[0].ID); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		historyErrorCode(t, historyCall(t, f, h, "GET", path, nil, 0), 429, "history_export_busy")
		release()
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatal("released export did not finish")
		}
		old := historyDecode[HistoryExport](t, w.ResponseRecorder, 200)
		if old.Revision != 0 || old.TotalCount != 5 || old.RecordedMinutes != 5 || len(old.Items) != 5 {
			t.Fatal("network-time writer changed the materialized export")
		}
		assertHistoryExportReleased(t, f)
		fresh := historyDecode[HistoryExport](t, historyCall(t, f, h, "GET", path, nil, 0), 200)
		if fresh.Revision != 1 || fresh.TotalCount != 6 || fresh.RecordedMinutes != 8 || len(fresh.Items) != 6 {
			t.Fatal("next export did not see the committed write")
		}
	})
	for _, mode := range []string{"cancel_rows", "deadline_rows", "deadline_header"} {
		t.Run("construction_"+mode, func(t *testing.T) {
			f := newReadPathFixture(t, 1)
			seedHistoryRows(t, f, 1, "No partial document")
			parent, stop := context.WithTimeout(context.Background(), 15*time.Second)
			defer stop()
			var ctx context.Context
			var cancel context.CancelFunc
			if strings.HasPrefix(mode, "deadline") {
				ctx, cancel = context.WithTimeout(parent, 3*time.Second)
			} else {
				ctx, cancel = context.WithCancel(parent)
			}
			defer cancel()
			arrived, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var entered, released sync.Once
			unblock := func() { released.Do(func() { close(release) }) }
			prefix := "SELECT te.id,te.task_id,"
			if strings.HasSuffix(mode, "header") {
				prefix = "SELECT p.title,p.revision,"
			}
			f.trace.barrier = func(qctx context.Context, sql string) {
				if phase, _ := qctx.Value(readPathPhaseKey{}).(string); phase == "history-construction" && strings.HasPrefix(sql, prefix) {
					entered.Do(func() { close(arrived) })
					select {
					case <-release:
					case <-qctx.Done():
					}
				}
			}
			w := httptest.NewRecorder()
			r := f.request("GET", "/projects/"+f.projects[0].ID+"/time-history/export", nil, "history-construction")
			requestCtx := context.WithValue(ctx, readPathPhaseKey{}, "history-construction")
			go func() { defer close(done); f.app.Handler().ServeHTTP(w, r.WithContext(requestCtx)) }()
			defer func() {
				unblock()
				cancel()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("canceled export did not join")
				}
			}()
			select {
			case <-arrived:
			case <-parent.Done():
				t.Fatal("export did not reach the controlled row-read barrier")
			}
			if strings.HasPrefix(mode, "cancel") {
				cancel()
			}
			select {
			case <-done:
			case <-parent.Done():
				t.Fatal("export ignored cancellation/deadline")
			}
			if strings.HasPrefix(mode, "deadline") {
				historyErrorCode(t, w, 504, "history_export_timeout")
			}
			if w.Code == 200 || strings.Contains(w.Body.String(), `"items"`) || w.Header().Get("Content-Disposition") != "" {
				t.Fatal("canceled construction published a successful file")
			}
			f.trace.barrier = nil
			assertHistoryExportReleased(t, f)
		})
	}
}

func testHistoryExportCoherentMaterialization(t *testing.T) {
	f := newReadPathFixture(t, 1)
	seedHistoryRows(t, f, 5, "Before a concurrent fact")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	arrived, release := make(chan struct{}), make(chan struct{})
	readerDone, writerDone := make(chan struct{}), make(chan struct{})
	writerPID := make(chan uint32, 1)
	var entered, released sync.Once
	unblock := func() { released.Do(func() { close(release) }) }
	f.trace.barrier = func(qctx context.Context, sql string) {
		phase, _ := qctx.Value(readPathPhaseKey{}).(string)
		if phase == "history-rows-held" && strings.HasPrefix(sql, "SELECT te.id,te.task_id,") {
			entered.Do(func() { close(arrived) })
			select {
			case <-release:
			case <-qctx.Done():
			}
		}
		if phase == "history-fact-writer" && sql == "SELECT pg_advisory_xact_lock(789504)" {
			pid, _ := qctx.Value(readPathConnectionKey{}).(uint32)
			writerPID <- pid
		}
	}
	h, path := f.app.Handler(), "/projects/"+f.projects[0].ID
	w := httptest.NewRecorder()
	go func() {
		defer close(readerDone)
		r := f.request("GET", path+"/time-history/export", nil, "history-rows-held")
		h.ServeHTTP(w, r.WithContext(context.WithValue(ctx, readPathPhaseKey{}, "history-rows-held")))
	}()
	writerStarted := false
	defer func() {
		unblock()
		cancel()
		select {
		case <-readerDone:
		case <-time.After(5 * time.Second):
			t.Error("export reader did not join")
		}
		if writerStarted {
			select {
			case <-writerDone:
			case <-time.After(5 * time.Second):
				t.Error("fact writer did not join")
			}
		}
	}()
	select {
	case <-arrived:
	case <-ctx.Done():
		t.Fatal("export did not reach protected materialization")
	}
	writeResponse := httptest.NewRecorder()
	writerStarted = true
	go func() {
		defer close(writerDone)
		fact := TimeEntry{TaskID: "profile-task-000", EmployeeID: "profile-employee-00", Date: "2024-02-29", Minutes: 3, Comment: "After protected materialization"}
		r := f.request("POST", path+"/time-entries", fact, "history-fact-writer")
		h.ServeHTTP(writeResponse, r.WithContext(context.WithValue(ctx, readPathPhaseKey{}, "history-fact-writer")))
	}()
	var pid uint32
	select {
	case pid = <-writerPID:
	case <-writerDone:
		t.Fatal("fact writer finished before the observed lock wait", writeResponse.Code)
	case <-ctx.Done():
		t.Fatal("fact writer did not reach capacity lock")
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
		case <-writerDone:
			t.Fatal("fact writer bypassed materialization lock")
		case <-ctx.Done():
			t.Fatal("fact writer wait was not observed")
		default:
		}
	}
	unblock()
	select {
	case <-readerDone:
	case <-ctx.Done():
		t.Fatal("materialized export did not finish")
	}
	select {
	case <-writerDone:
	case <-ctx.Done():
		t.Fatal("released fact writer did not finish")
	}
	_ = historyDecode[TimeEntry](t, writeResponse, 201)
	old := historyDecode[HistoryExport](t, w, 200)
	if old.Revision != 0 || old.TotalCount != 5 || old.RecordedMinutes != 5 || len(old.Items) != 5 {
		t.Fatal("mixed revision/totals/items in export")
	}
	fresh := historyDecode[HistoryPage](t, historyCall(t, f, h, "GET", path+"/time-history", nil, 0), 200)
	if fresh.Revision != 1 || fresh.TotalCount != 6 || fresh.RecordedMinutes != 8 {
		t.Fatal("writer did not become visible after export")
	}
	assertHistoryExportReleased(t, f)
}
