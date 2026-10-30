package planner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// A real shared-lock preview must see old inputs; the following real save must
// either recalculate changed global inputs or reject an obsolete project revision.
func TestReadPathConcurrentCapacityInputs(t *testing.T) {
	for _, change := range []string{"global_employee_maximum", "project_participation", "selected_calendar_version"} {
		t.Run(change, func(t *testing.T) {
			count := 1
			if change == "global_employee_maximum" {
				count = 2
			}
			f := newReadPathFixture(t, count)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if _, err := f.store.DB.Exec(ctx, "UPDATE users SET admin=true WHERE id='profile-user-09'"); err != nil {
				t.Fatal(err)
			}
			handler := f.app.Handler()
			request := func(method, path string, body any, phase string, admin bool) *http.Request {
				r := f.request(method, path, body, phase)
				if admin {
					r.Header.Del("Cookie")
					r.AddCookie(&http.Cookie{Name: "__Host-gma", Value: f.sessions[9].cookie})
					r.Header.Set("X-CSRF-Token", f.sessions[9].csrf)
				}
				return r.WithContext(context.WithValue(ctx, readPathPhaseKey{}, phase))
			}
			decode := func(response *httptest.ResponseRecorder) Project {
				t.Helper()
				var p Project
				if err := json.Unmarshal(response.Body.Bytes(), &p); err != nil {
					t.Fatal(err)
				}
				return p
			}
			clone := func(p Project) Project {
				t.Helper()
				raw, _ := json.Marshal(p)
				var copy Project
				if err := json.Unmarshal(raw, &copy); err != nil {
					t.Fatal(err)
				}
				return copy
			}
			path := "/projects/" + f.projects[0].ID
			candidate := clone(f.projects[0])
			writerPath, writerBody, writerAdmin := path, any(saveRequest{Project: candidate}), false
			if change == "global_employee_maximum" {
				writerPath = "/employees"
				writerBody = Employee{ID: "profile-employee-00", Name: "profile-employee-00", Daily: 144}
				writerAdmin = true
			} else if change == "project_participation" {
				for day, activity := range candidate.People[0].Week {
					activity.Daily = 48
					candidate.People[0].Week[day] = activity
				}
				writerBody = saveRequest{Project: candidate}
			} else {
				calendar := annualFixture(t, 2027)
				calendar.ID = "synthetic-2027-capacity-race-replacement"
				calendar.Days["2027-01-01"], calendar.Days["2027-01-04"] = Day{}, Day{}
				recalculateChecksum(t, &calendar)
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request("POST", "/calendars", calendar, "seed-replacement", true))
				if response.Code != 201 {
					t.Fatal("immutable replacement setup failed", response.Code, response.Body.String())
				}
				candidate.CalendarVersions["2027"] = calendar.ID
				writerBody = saveRequest{Project: candidate}
			}
			arrived, release := make(chan struct{}), make(chan struct{})
			writerPID := make(chan uint32, 1)
			var holdOnce, releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			f.trace.barrier = func(queryCtx context.Context, sql string) {
				phase, _ := queryCtx.Value(readPathPhaseKey{}).(string)
				if phase == "held-capacity-preview" && strings.HasPrefix(sql, "SELECT task_id,sum(minutes) FROM time_entries") {
					holdOnce.Do(func() {
						close(arrived)
						select {
						case <-release:
						case <-queryCtx.Done():
						}
					})
				}
				if phase == "capacity-input-writer" && sql == "SELECT pg_advisory_xact_lock(789504)" {
					pid, _ := queryCtx.Value(readPathConnectionKey{}).(uint32)
					writerPID <- pid
				}
			}
			previewResponse, previewDone := httptest.NewRecorder(), make(chan struct{})
			go func() {
				defer close(previewDone)
				handler.ServeHTTP(previewResponse, request("POST", path+"/preview", saveRequest{Project: f.projects[0]}, "held-capacity-preview", false))
			}()
			defer func() {
				unblock()
				cancel()
				select {
				case <-previewDone:
				case <-time.After(5 * time.Second):
					t.Error("held preview did not terminate during cleanup")
				}
			}()
			select {
			case <-arrived:
			case <-ctx.Done():
				t.Fatal("preview did not reach the input barrier")
			}
			writerResponse, writerDone := httptest.NewRecorder(), make(chan struct{})
			go func() {
				defer close(writerDone)
				method := "PUT"
				if writerAdmin {
					method = "POST"
				}
				handler.ServeHTTP(writerResponse, request(method, writerPath, writerBody, "capacity-input-writer", writerAdmin))
			}()
			defer func() {
				unblock()
				cancel()
				select {
				case <-writerDone:
				case <-time.After(5 * time.Second):
					t.Error("input writer did not terminate during cleanup")
				}
			}()
			var pid uint32
			select {
			case pid = <-writerPID:
			case <-writerDone:
				t.Fatal("input writer completed before the shared preview lock was released", writerResponse.Code)
			case <-ctx.Done():
				t.Fatal("input writer did not reach the portfolio lock")
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
					t.Fatal("capacity input changed while preview's old snapshot was held")
				case <-ctx.Done():
					t.Fatal("input writer did not enter an observed database wait")
				default:
				}
			}
			var beforeRevision int64
			if err := f.store.DB.QueryRow(ctx, "SELECT revision FROM projects WHERE id=$1", f.projects[0].ID).Scan(&beforeRevision); err != nil || beforeRevision != 0 {
				t.Fatal("held project was changed", beforeRevision, err)
			}
			unblock()
			for _, done := range []chan struct{}{previewDone, writerDone} {
				select {
				case <-done:
				case <-ctx.Done():
					t.Fatal("released preview/input writer did not finish")
				}
			}
			if previewResponse.Code != 200 || writerResponse.Code != 200 {
				t.Fatal("controlled API call failed", previewResponse.Code, writerResponse.Code, previewResponse.Body.String(), writerResponse.Body.String())
			}
			oldPreview := decode(previewResponse)
			if oldPreview.Revision != beforeRevision || oldPreview.CalculationState != "ready" || len(oldPreview.Loads) != 600 || oldPreview.Tasks[270].End != "2027-01-11" || oldPreview.People[0].Week["1"].Daily != 96 || !reflect.DeepEqual(oldPreview.CalendarVersions, f.projects[0].CalendarVersions) {
				t.Fatal("preview did not retain the old coherent capacity/calendar inputs", oldPreview.Revision, oldPreview.CalculationState, oldPreview.Tasks[270].End)
			}
			assertEmployeeLoads := func(p Project, dates []string, minutes int) {
				t.Helper()
				got := map[string]int{}
				for _, load := range p.Loads {
					if load.EmployeeID == "profile-employee-00" {
						got[load.Date] += load.Minutes
					}
				}
				if len(got) != len(dates) {
					t.Fatal("unexpected employee load days", len(got), len(dates))
				}
				for _, day := range dates {
					if got[day] != minutes {
						t.Fatal("unexpected daily load", day, got[day], minutes)
					}
				}
			}
			oldDates := strings.Fields("2026-12-14 2026-12-15 2026-12-16 2026-12-17 2026-12-18 2026-12-21 2026-12-22 2026-12-23 2026-12-24 2026-12-25 2026-12-28 2026-12-29 2026-12-30 2026-12-31 2027-01-04 2027-01-05 2027-01-06 2027-01-07 2027-01-08 2027-01-11")
			assertEmployeeLoads(oldPreview, oldDates, 96)
			saveResponse := httptest.NewRecorder()
			handler.ServeHTTP(saveResponse, request("PUT", path, saveRequest{Project: oldPreview}, "save-old-preview", false))
			if change == "global_employee_maximum" {
				var stillRevision int64
				if err := f.store.DB.QueryRow(ctx, "SELECT revision FROM projects WHERE id=$1", f.projects[1].ID).Scan(&stillRevision); err != nil || stillRevision != 0 {
					t.Fatal("global employee change incorrectly bumped another project revision", stillRevision, err)
				}
				updated := decode(saveResponse)
				warning := strings.Join(updated.Warnings, " ")
				if saveResponse.Code != 200 || updated.Revision != beforeRevision+1 || updated.CalculationState != "blocked_capacity" || len(updated.Loads) != 0 || updated.Tasks[0].Start != "" || updated.Tasks[0].End != "" || !strings.Contains(warning, "192") || !strings.Contains(warning, "144") || !strings.Contains(warning, "2026-12-14") {
					t.Fatal("old preview bypassed changed global capacity", saveResponse.Code, updated.CalculationState, updated.Revision, warning)
				}
			} else {
				var failure struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				if json.Unmarshal(saveResponse.Body.Bytes(), &failure) != nil || saveResponse.Code != 409 || failure.Error.Code != "revision_conflict" {
					t.Fatal("old preview overwrote a new project input revision", saveResponse.Code, saveResponse.Body.String())
				}
				updated := decode(writerResponse)
				if updated.Revision != beforeRevision+1 || updated.CalculationState != "ready" {
					t.Fatal("capacity input writer did not publish a new coherent project", updated.Revision, updated.CalculationState)
				}
				if change == "project_participation" {
					if updated.People[0].Week["1"].Daily != 48 || updated.Tasks[270].End != "2027-02-08" || len(updated.Loads) != 620 {
						t.Fatal("lower participation did not extend the employee's exact schedule", updated.Tasks[270].End, len(updated.Loads))
					}
					var total, days int
					for _, load := range updated.Loads {
						if load.EmployeeID == "profile-employee-00" {
							if load.Minutes != 48 {
								t.Fatal("new quota exceeded", load)
							}
							total += load.Minutes
							days++
						}
					}
					if total != 1920 || days != 40 {
						t.Fatal("participation effort changed", total, days)
					}
				} else {
					if updated.Tasks[299].End != "2027-01-12" || updated.CalendarVersions["2027"] != candidate.CalendarVersions["2027"] || len(updated.Loads) != 600 {
						t.Fatal("calendar replacement did not move the exact schedule", updated.Tasks[299].End, updated.CalendarVersions)
					}
					newDates := []string{}
					for _, day := range oldDates {
						if day != "2027-01-04" {
							newDates = append(newDates, day)
						}
					}
					newDates = append(newDates, "2027-01-12")
					assertEmployeeLoads(updated, newDates, 96)
				}
				stored, err := readProject(ctx, f.store.DB, updated.ID)
				if err != nil || stored.Revision != updated.Revision || stored.Tasks[270].End != updated.Tasks[270].End || !reflect.DeepEqual(stored.CalendarVersions, updated.CalendarVersions) || stored.People[0].Week["1"].Daily != updated.People[0].Week["1"].Daily {
					t.Fatal("stale preview changed the persisted new settings", err, stored.Revision, stored.Tasks[270].End)
				}
			}
			t.Logf("Concurrent input %s: observed database wait; old preview coherent; subsequent save %d checked authoritative new inputs", change, saveResponse.Code)
		})
	}
}
