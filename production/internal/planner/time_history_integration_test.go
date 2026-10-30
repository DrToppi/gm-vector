package planner

import (
	"context"
	"fmt"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestTimeHistoryLegacyMetadataRegression(t *testing.T) {
	f := newReadPathFixture(t, 1)
	f.addFacts(t)
	w := httptest.NewRecorder()
	f.app.Handler().ServeHTTP(w, f.request("GET", "/projects/"+f.projects[0].ID+"/time-entries", nil, ""))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w.Header().Get("X-History-Total-Count") != "3000" || w.Header().Get("X-History-Truncated") != "true" {
		t.Fatalf("history silently truncates3000 facts: total=%q truncated=%q", w.Header().Get("X-History-Total-Count"), w.Header().Get("X-History-Truncated"))
	}
}

func TestTimeHistoryPaginationIntegration(t *testing.T) {
	for _, n := range []int{0, 1, 999, 1000, 1001, 10000} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			f := newReadPathFixture(t, 1)
			seedHistoryRows(t, f, n, "Synthetic history")
			h := f.app.Handler()
			path := "/projects/" + f.projects[0].ID + "/time-history?limit=500"
			seen := 0
			next := ""
			for {
				request := path
				if next != "" {
					request += "&cursor=" + url.QueryEscape(next)
				}
				page := historyDecode[HistoryPage](t, historyCall(t, f, h, "GET", request, nil, 0), 200)
				if page.TotalCount != int64(n) || page.RecordedMinutes != int64(n) || page.Limit != 500 || page.Order != historyOrder || page.PlanningTimezone != defaultPlanningTimezone || page.Items == nil {
					t.Fatal("incorrect full-history metadata", page.HistoryCommon)
				}
				for _, entry := range page.Items {
					seen++
					if entry.ID != fmt.Sprintf("history-%06d", seen) || entry.Date != "2024-02-29" || entry.TaskTitle != "profile-task-000" || entry.Minutes != 1 {
						t.Fatal("lost/repeated/out-of-order item", seen, entry)
					}
					if seen == n && (entry.EmployeeID != "" || entry.EmployeeName != "") {
						t.Fatal("NULL employee was invented", entry)
					}
				}
				if page.NextCursor == nil {
					break
				}
				if len(page.Items) != 500 {
					t.Fatal("short intermediate page")
				}
				next = *page.NextCursor
			}
			if seen != n {
				t.Fatal("history traversal incomplete", seen, n)
			}
			legacy := historyCall(t, f, h, "GET", "/projects/"+f.projects[0].ID+"/time-entries", nil, 0)
			if legacy.Header().Get("X-History-Total-Count") != fmt.Sprint(n) || legacy.Header().Get("X-History-Truncated") != fmt.Sprint(n > 1000) {
				t.Fatal("legacy truncation metadata", legacy.Header())
			}
		})
	}
	t.Run("inclusive_subtree_legacy_and_removed_assignee", func(t *testing.T) {
		f := newReadPathFixture(t, 1)
		ctx := context.Background()
		id := f.projects[0].ID
		for task, legacy := range map[string]int{"profile-task-000": 11, "profile-task-030": 13, "profile-task-060": 17, "profile-task-001": 19} {
			if _, e := f.store.DB.Exec(ctx, "UPDATE tasks SET legacy_actual_minutes=$3 WHERE project_id=$1 AND id=$2", id, task, legacy); e != nil {
				t.Fatal(e)
			}
		}
		if _, e := f.store.DB.Exec(ctx, `INSERT INTO time_entries(id,project_id,task_id,employee_id,work_date,minutes,actor) VALUES('root-old',$1,'profile-task-000','profile-employee-00','1999-12-31',10,'profile-user-00'),('child',$1,'profile-task-030','profile-employee-00','2024-02-29',20,'profile-user-00'),('grandchild',$1,'profile-task-060','profile-employee-00','2025-01-01',30,'profile-user-00'),('unrelated',$1,'profile-task-001','profile-employee-00','2024-02-29',40,'profile-user-00'),('root-unattributed',$1,'profile-task-000',NULL,'2024-02-29',5,'profile-user-00')`, id); e != nil {
			t.Fatal(e)
		}
		p := f.projects[0]
		p.Tasks = []Task{p.Tasks[0], p.Tasks[30], p.Tasks[60], p.Tasks[1]}
		p.Links = []Link{}
		p.People = []Participation{}
		for i := range p.Tasks {
			p.Tasks[i].Assignments = []Assignment{}
			p.Tasks[i].Planned = 0
			p.Tasks[i].DurationDays = 1
		}
		p.Tasks[1].Parent = p.Tasks[0].ID
		p.Tasks[2].Parent = p.Tasks[1].ID
		h := f.app.Handler()
		saved := historyDecode[Project](t, historyCall(t, f, h, "PUT", "/projects/"+id, saveRequest{Project: p}, 0), 200)
		if saved.Tasks[0].Actual != 47 {
			t.Fatal("existing summary Actual policy changed", saved.Tasks[0].Actual)
		}
		for _, test := range []struct {
			query          string
			count, minutes int64
		}{{"", 4, 65}, {"&employee_id=profile-employee-00", 3, 60}, {"&date_from=2024-02-29&date_to=2024-02-29", 2, 25}, {"&employee_id=profile-employee-00&date_from=2024-02-29&date_to=2024-02-29", 1, 20}, {"&employee_id=removed-no-history", 0, 0}} {
			page := historyDecode[HistoryPage](t, historyCall(t, f, h, "GET", "/projects/"+id+"/time-history?task_id=profile-task-000"+test.query, nil, 0), 200)
			if page.TotalCount != test.count || page.RecordedMinutes != test.minutes || page.Legacy.Minutes != 41 || page.Legacy.Scope != "task_subtree" || page.Legacy.Attribution != "unavailable" {
				t.Fatal("subtree/filter/legacy attribution", test.query, page.HistoryCommon)
			}
		}
	})
}
