package planner

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type HistoryFilters struct {
	TaskID     string `json:"task_id"`
	EmployeeID string `json:"employee_id"`
	DateFrom   string `json:"date_from"`
	DateTo     string `json:"date_to"`
}
type HistoryItem struct {
	TimeEntry
	TaskTitle    string `json:"task_title"`
	EmployeeName string `json:"employee_name"`
}
type HistoryLegacy struct {
	Minutes     int64  `json:"minutes"`
	Scope       string `json:"scope"`
	Attribution string `json:"employee_date_attribution"`
}
type HistoryCommon struct {
	ProjectID        string         `json:"project_id"`
	ProjectTitle     string         `json:"project_title"`
	Revision         int64          `json:"revision"`
	PlanningTimezone string         `json:"planning_timezone"`
	Filters          HistoryFilters `json:"filters"`
	Order            string         `json:"order"`
	TotalCount       int64          `json:"total_count"`
	RecordedMinutes  int64          `json:"recorded_minutes"`
	Legacy           HistoryLegacy  `json:"legacy_unattributed"`
}
type HistoryPage struct {
	HistoryCommon
	Limit      int           `json:"limit"`
	Items      []HistoryItem `json:"items"`
	NextCursor *string       `json:"next_cursor"`
}
type HistoryExport struct {
	HistoryCommon
	Format        string        `json:"format"`
	SchemaVersion int           `json:"schema_version"`
	GeneratedAt   string        `json:"generated_at"`
	Items         []HistoryItem `json:"items"`
}

func parseHistoryFilters(q url.Values, paged bool) (HistoryFilters, int, error) {
	f := HistoryFilters{q.Get("task_id"), q.Get("employee_id"), q.Get("date_from"), q.Get("date_to")}
	allowed := map[string]bool{"task_id": true, "employee_id": true, "date_from": true, "date_to": true}
	if paged {
		allowed["limit"] = true
		allowed["cursor"] = true
	}
	for key, values := range q {
		if !allowed[key] || len(values) != 1 {
			return f, 0, bad("Invalid history query")
		}
	}
	if (f.TaskID != "" && !validID.MatchString(f.TaskID)) || (f.EmployeeID != "" && !validID.MatchString(f.EmployeeID)) {
		return f, 0, bad("Invalid history filter")
	}
	for _, s := range []string{f.DateFrom, f.DateTo} {
		if s != "" {
			if parsed, err := date(s); err != nil || parsed.Year() < 1 || fmtDate(parsed) != s {
				return f, 0, bad("Invalid history date")
			}
		}
	}
	if f.DateFrom != "" && f.DateTo != "" && f.DateFrom > f.DateTo {
		return f, 0, bad("History date range is reversed")
	}
	limit := 100
	if paged && q.Has("limit") {
		n, e := strconv.Atoi(q.Get("limit"))
		if e != nil || n < 1 || n > 500 {
			return f, 0, bad("History page size must be1–500")
		}
		limit = n
	}
	return f, limit, nil
}
func notFoundHistory() error { return appError{404, "not_found", "Project or task not found"} }

// Current read access is checked on the held transaction's own connection;
// viewers remain readers and a cursor never grants authorization.
func (a *App) historyHeader(ctx context.Context, q DBTX, u Principal, id string) (HistoryCommon, string, error) {
	h := HistoryCommon{ProjectID: id, PlanningTimezone: a.planningTimezone(), Order: historyOrder, Legacy: HistoryLegacy{Scope: "project", Attribution: "unavailable"}}
	if u.Token && !contains(u.Projects, id) {
		return h, "", notFoundHistory()
	}
	var role string
	err := q.QueryRow(ctx, `SELECT p.title,p.revision,CASE WHEN u.admin THEN 'manager' ELSE coalesce(m.role,'') END FROM projects p JOIN users u ON u.id=$2 AND u.active LEFT JOIN memberships m ON m.project_id=p.id AND m.user_id=u.id WHERE p.id=$1 AND p.archived_at IS NULL`, id, u.ID).Scan(&h.ProjectTitle, &h.Revision, &role)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && role == "" {
		return h, "", notFoundHistory()
	}
	return h, role, err
}
func historyScope(ctx context.Context, q DBTX, id string, filters HistoryFilters, h *HistoryCommon) ([]string, error) {
	rows, e := q.Query(ctx, "SELECT id,coalesce(parent_id,''),legacy_actual_minutes FROM tasks WHERE project_id=$1", id)
	if e != nil {
		return nil, e
	}
	parents := map[string][]string{}
	legacy := map[string]int64{}
	for rows.Next() {
		var tid, parent string
		var n int64
		if e = rows.Scan(&tid, &parent, &n); e != nil {
			rows.Close()
			return nil, e
		}
		legacy[tid] = n
		parents[parent] = append(parents[parent], tid)
		if len(legacy) > 300 {
			rows.Close()
			return nil, bad("Project task limit exceeded")
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	selected := map[string]bool{}
	if filters.TaskID == "" {
		for tid := range legacy {
			selected[tid] = true
		}
	} else {
		if _, ok := legacy[filters.TaskID]; !ok {
			return nil, notFoundHistory()
		}
		h.Legacy.Scope = "task_subtree"
		queue := []string{filters.TaskID}
		for len(queue) > 0 {
			tid := queue[0]
			queue = queue[1:]
			if selected[tid] {
				continue
			}
			selected[tid] = true
			queue = append(queue, parents[tid]...)
		}
	}
	ids := []string{}
	for tid := range selected {
		ids = append(ids, tid)
		h.Legacy.Minutes += legacy[tid]
	}
	sort.Strings(ids)
	h.Filters = filters
	return ids, nil
}

const historyWhere = `te.project_id=$1 AND te.task_id=ANY($2::text[]) AND ($3::text='' OR te.employee_id=$3) AND ($4::text='' OR te.work_date>=nullif($4,'')::date) AND ($5::text='' OR te.work_date<=nullif($5,'')::date)`
const historyColumns = `te.id,te.task_id,coalesce(te.employee_id,''),te.work_date::text,te.minutes,te.comment,t.title,coalesce(e.name,'')`
const historyJoins = ` FROM time_entries te JOIN tasks t ON t.project_id=te.project_id AND t.id=te.task_id LEFT JOIN employees e ON e.id=te.employee_id WHERE `

func historyArguments(id string, ids []string, f HistoryFilters) []any {
	return []any{id, ids, f.EmployeeID, f.DateFrom, f.DateTo}
}
func historyTotals(ctx context.Context, q DBTX, args []any, h *HistoryCommon) error {
	return q.QueryRow(ctx, "SELECT count(*),coalesce(sum(te.minutes),0) FROM time_entries te WHERE "+historyWhere, args...).Scan(&h.TotalCount, &h.RecordedMinutes)
}
func scanHistoryItem(rows pgx.Rows) (HistoryItem, error) {
	var v HistoryItem
	e := rows.Scan(&v.ID, &v.TaskID, &v.EmployeeID, &v.Date, &v.Minutes, &v.Comment, &v.TaskTitle, &v.EmployeeName)
	return v, e
}
func (a *App) timeHistory(w http.ResponseWriter, r *http.Request, u Principal) error {
	query, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		return bad("Invalid history query")
	}
	f, limit, e := parseHistoryFilters(query, true)
	if e != nil {
		return e
	}
	ctx := r.Context()
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = capacityLock(ctx, tx, true); e != nil {
		return e
	}
	h, _, e := a.historyHeader(ctx, tx, u, r.PathValue("id"))
	if e != nil {
		return e
	}
	now := time.Now().UTC()
	cursor := historyCursor{Issued: now.Unix(), Expires: now.Add(historyCursorTTL).Unix()}
	if raw := query.Get("cursor"); raw != "" {
		cursor, e = decodeHistoryCursor(raw, a.Config.Secret, h.ProjectID, u, f, limit, now)
		if e != nil {
			return e
		}
		if cursor.Revision != h.Revision {
			return appError{409, "history_changed", "History changed; refresh the list"}
		}
	}
	ids, e := historyScope(ctx, tx, h.ProjectID, f, &h)
	if e != nil {
		return e
	}
	args := historyArguments(h.ProjectID, ids, f)
	if e = historyTotals(ctx, tx, args, &h); e != nil {
		return e
	}
	page := HistoryPage{HistoryCommon: h, Limit: limit, Items: []HistoryItem{}}
	args = append(args, cursor.LastDate, cursor.LastID, limit+1)
	rows, e := tx.Query(ctx, "SELECT "+historyColumns+historyJoins+historyWhere+` AND ($6::text='' OR te.work_date<nullif($6,'')::date OR(te.work_date=nullif($6,'')::date AND te.id>$7)) ORDER BY te.work_date DESC,te.id ASC LIMIT $8`, args...)
	if e != nil {
		return e
	}
	for rows.Next() {
		v, err := scanHistoryItem(rows)
		if err != nil {
			rows.Close()
			return err
		}
		page.Items = append(page.Items, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[len(page.Items)-1]
		cursor.Version = 1
		cursor.Project = h.ProjectID
		cursor.Principal = u.ID
		cursor.Auth = cursorAuth(u)
		cursor.Filters = f
		cursor.Limit = limit
		cursor.Order = historyOrder
		cursor.Revision = h.Revision
		cursor.LastDate = last.Date
		cursor.LastID = last.ID
		next, err := encodeHistoryCursor(cursor, a.Config.Secret)
		if err != nil {
			return err
		}
		page.NextCursor = &next
	}
	raw, e := materializeProjectRead(ctx, tx, page)
	if e != nil {
		return e
	}
	writeMaterializedJSON(w, raw)
	return nil
}
