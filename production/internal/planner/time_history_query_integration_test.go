package planner

import "testing"

func TestTimeHistoryRawQueryIntegration(t *testing.T) {
	f := newReadPathFixture(t, 1)
	seedHistoryRows(t, f, 3, "Synthetic query validation")
	h := f.app.Handler()
	path := "/projects/" + f.projects[0].ID + "/time-history"
	before := f.digest(t)
	for _, suffix := range []string{"", "/export"} {
		t.Run("resource"+suffix, func(t *testing.T) {
			// Correctly escaped names and values retain their meaning: only the
			// two attributed facts match, while the third has no employee.
			query := "?task%5Fid=profile-task-000&employee%5Fid=%70rofile%2Demployee%2D00&date_from=2024%2D02%2D29"
			w := historyCall(t, f, h, "GET", path+suffix+query, nil, 0)
			if suffix == "" {
				page := historyDecode[HistoryPage](t, w, 200)
				if page.TotalCount != 2 || page.RecordedMinutes != 2 || len(page.Items) != 2 || page.Filters.EmployeeID != "profile-employee-00" || page.Filters.TaskID != "profile-task-000" || page.Filters.DateFrom != "2024-02-29" {
					t.Fatal("valid percent-encoding changed page filter meaning", page.HistoryCommon)
				}
			} else {
				doc := historyDecode[HistoryExport](t, w, 200)
				if doc.TotalCount != 2 || doc.RecordedMinutes != 2 || len(doc.Items) != 2 || doc.Filters.EmployeeID != "profile-employee-00" || doc.Filters.TaskID != "profile-task-000" || doc.Filters.DateFrom != "2024-02-29" {
					t.Fatal("valid percent-encoding changed export filter meaning", doc.HistoryCommon)
				}
			}
			for _, query := range []string{
				"employee_id=%zz",
				"employee_id=%",
				"%zz=profile-employee-00",
				"unexpected=%zz",
				"date_from=2024-02-29&employee_id=%zz",
				"employee_id=not-present;ignored=1",
				"date_from=2024-02-29&employee_id=not-present;ignored=1",
				"employee_id=profile-employee-00%3Bignored%3D1",
			} {
				t.Run(query, func(t *testing.T) {
					historyErrorCode(t, historyCall(t, f, h, "GET", path+suffix+"?"+query, nil, 0), 422, "validation")
				})
			}
		})
	}
	if f.digest(t) != before {
		t.Fatal("history query validation modified persisted project data")
	}
}
