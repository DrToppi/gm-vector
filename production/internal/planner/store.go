package planner

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"sort"
	"time"
)

//go:embed schema.sql schema-002.sql schema-003.sql schema-004.sql schema-005.sql schema-006.sql schema-007.sql schema-008.sql
var schema embed.FS

type Store struct{ DB *pgxpool.Pool }

func Open(ctx context.Context, url string) (*Store, error) {
	cfg, e := pgxpool.ParseConfig(url)
	if e != nil {
		return nil, e
	}
	cfg.MaxConns = 12
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "10000"
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		return nil, e
	}
	if e = db.Ping(ctx); e != nil {
		db.Close()
		return nil, e
	}
	return &Store{db}, nil
}
func (s *Store) Migrate(ctx context.Context) error { return s.MigrateForRuntime(ctx, "") }

func (s *Store) MigrateForRuntime(ctx context.Context, runtimeRole string) error {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(789501)"); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_versions(version integer PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())"); e != nil {
		return e
	}
	for i, name := range []string{"schema.sql", "schema-002.sql", "schema-003.sql", "schema-004.sql", "schema-005.sql", "schema-006.sql", "schema-007.sql", "schema-008.sql"} {
		b, e := schema.ReadFile(name)
		if e != nil {
			return e
		}
		sum := sha256.Sum256(b)
		digest := hex.EncodeToString(sum[:])
		var old string
		e = tx.QueryRow(ctx, "SELECT checksum FROM schema_versions WHERE version=$1", i+1).Scan(&old)
		if e == nil {
			if old != digest {
				return errors.New("schema checksum changed: explicit migration required")
			}
			continue
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if _, e = tx.Exec(ctx, string(b)); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "INSERT INTO schema_versions(version,checksum) VALUES($1,$2)", i+1, digest); e != nil {
			return e
		}
	}
	if runtimeRole != "" {
		if e = grantRuntime(ctx, tx, runtimeRole); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}

type DBTX interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func readProject(ctx context.Context, q DBTX, id string) (Project, error) {
	p := Project{Tasks: []Task{}, Links: []Link{}, People: []Participation{}}
	var pins []byte
	e := q.QueryRow(ctx, "SELECT id,title,brief,start_date::text,duration_mode,calendar_id,revision,external_reference,calendar_versions FROM projects WHERE id=$1 AND archived_at IS NULL", id).Scan(&p.ID, &p.Title, &p.Brief, &p.Start, &p.Mode, &p.CalendarID, &p.Revision, &p.ExternalReference, &pins)
	if e != nil {
		return p, e
	}
	if e = json.Unmarshal(pins, &p.CalendarVersions); e != nil {
		return p, e
	}
	rows, e := q.Query(ctx, `SELECT id,coalesce(parent_id,''),title,description,result,brief,status,coalesce(requested_start::text,''),coalesce(start_date::text,''),coalesce(end_date::text,''),planned_minutes,responsibility,customer_owner,coalesce(customer_due::text,''),owner_legacy,coalesce(legacy_start::text,''),coalesce(legacy_end::text,''),legacy_actual_minutes,duration_days,customer_email FROM tasks WHERE project_id=$1 ORDER BY sort_order,id`, id)
	if e != nil {
		return p, e
	}
	for rows.Next() {
		var t Task
		t.Assignments = []Assignment{}
		e = rows.Scan(&t.ID, &t.Parent, &t.Title, &t.Description, &t.Result, &t.Brief, &t.Status, &t.RequestedStart, &t.Start, &t.End, &t.Planned, &t.Responsibility, &t.CustomerOwner, &t.CustomerDue, &t.OwnerLegacy, &t.LegacyStart, &t.LegacyEnd, &t.LegacyActual, &t.DurationDays, &t.CustomerEmail)
		if e != nil {
			rows.Close()
			return p, e
		}
		t.Actual = t.LegacyActual
		p.Tasks = append(p.Tasks, t)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return p, e
	}
	idx := map[string]int{}
	for i, t := range p.Tasks {
		idx[t.ID] = i
	}
	rows, e = q.Query(ctx, "SELECT task_id,employee_id,daily_minutes FROM assignments WHERE project_id=$1 ORDER BY employee_id", id)
	if e != nil {
		return p, e
	}
	for rows.Next() {
		var tid string
		var a Assignment
		if e = rows.Scan(&tid, &a.EmployeeID, &a.Daily); e != nil {
			rows.Close()
			return p, e
		}
		p.Tasks[idx[tid]].Assignments = append(p.Tasks[idx[tid]].Assignments, a)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return p, e
	}
	rows, e = q.Query(ctx, "SELECT predecessor,successor,mode,lag_days FROM dependencies WHERE project_id=$1 ORDER BY predecessor,successor", id)
	if e != nil {
		return p, e
	}
	for rows.Next() {
		var l Link
		if e = rows.Scan(&l.From, &l.To, &l.Mode, &l.Days); e != nil {
			rows.Close()
			return p, e
		}
		p.Links = append(p.Links, l)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return p, e
	}
	rows, e = q.Query(ctx, "SELECT employee_id,weekdays,window_start,window_end,exceptions,week FROM participation WHERE project_id=$1 ORDER BY employee_id", id)
	if e != nil {
		return p, e
	}
	for rows.Next() {
		var v Participation
		var raw, week []byte
		if e = rows.Scan(&v.EmployeeID, &v.Weekdays, &v.WindowStart, &v.WindowEnd, &raw, &week); e != nil {
			rows.Close()
			return p, e
		}
		if e = json.Unmarshal(raw, &v.Exceptions); e != nil {
			rows.Close()
			return p, e
		}
		if e = json.Unmarshal(week, &v.Week); e != nil {
			rows.Close()
			return p, e
		}
		p.People = append(p.People, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return p, e
	}
	rows, e = q.Query(ctx, "SELECT task_id,sum(minutes) FROM time_entries WHERE project_id=$1 GROUP BY task_id", id)
	if e != nil {
		return p, e
	}
	for rows.Next() {
		var tid string
		var n int
		if e = rows.Scan(&tid, &n); e != nil {
			rows.Close()
			return p, e
		}
		p.Tasks[idx[tid]].Actual += n
	}
	e = rows.Err()
	rows.Close()
	return p, e
}
func readEmployees(ctx context.Context, q DBTX) (map[string]Employee, error) {
	out := map[string]Employee{}
	rows, e := q.Query(ctx, "SELECT id,name,daily_minutes,coalesce(user_id,''),provider,external_id,email FROM employees ORDER BY name,id")
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var v Employee
		if e = rows.Scan(&v.ID, &v.Name, &v.Daily, &v.UserID, &v.Provider, &v.ExternalID, &v.Email); e != nil {
			return out, e
		}
		out[v.ID] = v
	}
	return out, rows.Err()
}
func readCalendar(ctx context.Context, q DBTX, id string) (Calendar, error) {
	var c Calendar
	var raw []byte
	e := q.QueryRow(ctx, "SELECT id,year,title,source,checksum,days FROM calendars WHERE id=$1", id).Scan(&c.ID, &c.Year, &c.Title, &c.Source, &c.Checksum, &raw)
	if e == nil {
		e = json.Unmarshal(raw, &c.Days)
	}
	return c, e
}
func audit(ctx context.Context, q DBTX, actor, project, action, req string, details any) error {
	b, e := json.Marshal(details)
	if e != nil {
		return e
	}
	_, e = q.Exec(ctx, "INSERT INTO audit(actor,project_id,action,details,request_id) VALUES($1,$2,$3,$4,$5)", nullable(actor), nullable(project), action, b, req)
	return e
}
func writeProject(ctx context.Context, tx pgx.Tx, p Project) error {
	if p.CalendarVersions == nil {
		p.CalendarVersions = map[string]string{}
	}
	pins, err := json.Marshal(p.CalendarVersions)
	if err != nil {
		return err
	}
	_, e := tx.Exec(ctx, "UPDATE projects SET title=$2,brief=$3,start_date=$4,duration_mode=$5,calendar_id=$6,revision=$7,external_reference=$8,calendar_versions=$9 WHERE id=$1", p.ID, p.Title, p.Brief, p.Start, p.Mode, p.CalendarID, p.Revision, p.ExternalReference, pins)
	if e != nil {
		return e
	}
	// Upsert parents and children with deferred FK, preserving time entries on retained tasks.
	ids := []string{}
	for i, t := range p.Tasks {
		ids = append(ids, t.ID)
		_, e = tx.Exec(ctx, `INSERT INTO tasks(project_id,id,parent_id,title,description,result,brief,status,requested_start,start_date,end_date,planned_minutes,responsibility,customer_owner,customer_due,owner_legacy,legacy_start,legacy_end,legacy_actual_minutes,sort_order,duration_days,customer_email) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22) ON CONFLICT(project_id,id) DO UPDATE SET parent_id=excluded.parent_id,title=excluded.title,description=excluded.description,result=excluded.result,brief=excluded.brief,status=excluded.status,requested_start=excluded.requested_start,start_date=excluded.start_date,end_date=excluded.end_date,planned_minutes=excluded.planned_minutes,responsibility=excluded.responsibility,customer_owner=excluded.customer_owner,customer_due=excluded.customer_due,owner_legacy=excluded.owner_legacy,legacy_start=excluded.legacy_start,legacy_end=excluded.legacy_end,legacy_actual_minutes=excluded.legacy_actual_minutes,sort_order=excluded.sort_order,duration_days=excluded.duration_days,customer_email=excluded.customer_email`, p.ID, t.ID, nullable(t.Parent), t.Title, t.Description, t.Result, t.Brief, t.Status, nullable(t.RequestedStart), nullable(t.Start), nullable(t.End), t.Planned, t.Responsibility, t.CustomerOwner, nullable(t.CustomerDue), t.OwnerLegacy, nullable(t.LegacyStart), nullable(t.LegacyEnd), t.LegacyActual, i, t.DurationDays, t.CustomerEmail)
		if e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, "DELETE FROM dependencies WHERE project_id=$1", p.ID); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "DELETE FROM assignments WHERE project_id=$1", p.ID); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "DELETE FROM tasks WHERE project_id=$1 AND NOT(id=ANY($2::text[]))", p.ID, ids); e != nil {
		return e
	}
	for _, l := range p.Links {
		if _, e = tx.Exec(ctx, "INSERT INTO dependencies VALUES($1,$2,$3,$4,$5)", p.ID, l.From, l.To, l.Mode, l.Days); e != nil {
			return e
		}
	}
	for _, t := range p.Tasks {
		for _, a := range t.Assignments {
			if _, e = tx.Exec(ctx, "INSERT INTO assignments VALUES($1,$2,$3,$4)", p.ID, t.ID, a.EmployeeID, a.Daily); e != nil {
				return e
			}
		}
	}
	if _, e = tx.Exec(ctx, "DELETE FROM participation WHERE project_id=$1", p.ID); e != nil {
		return e
	}
	for _, v := range p.People {
		raw, _ := json.Marshal(v.Exceptions)
		week := []byte("{}")
		if v.Week != nil {
			week, _ = json.Marshal(v.Week)
		}
		days := v.Weekdays
		if days == nil {
			days = []int{}
		}
		if _, e = tx.Exec(ctx, "INSERT INTO participation(project_id,employee_id,weekdays,window_start,window_end,exceptions,week) VALUES($1,$2,$3,$4,$5,$6,$7)", p.ID, v.EmployeeID, days, v.WindowStart, v.WindowEnd, raw, week); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) role(ctx context.Context, u Principal, id string) (string, error) {
	if u.Token && !contains(u.Projects, id) {
		return "", errors.New("forbidden")
	}
	if u.Admin {
		return "manager", nil
	}
	var role string
	e := s.DB.QueryRow(ctx, "SELECT role FROM memberships WHERE project_id=$1 AND user_id=$2", id, u.ID).Scan(&role)
	return role, e
}

// Every operation changing capacity takes this lock before any project row lock.
// Shared readers calculate against one stable set of project/employee settings.
func capacityLock(ctx context.Context, q DBTX, shared bool) error {
	sql := "SELECT pg_advisory_xact_lock(789504)"
	if shared {
		sql = "SELECT pg_advisory_xact_lock_shared(789504)"
	}
	_, err := q.Exec(ctx, sql)
	return err
}

func (s *Store) prepare(ctx context.Context, q DBTX, p *Project) (result error) {
	customerDates := customerInputRanges(p)
	defer func() {
		if result != nil {
			restoreCustomerInputRanges(p, customerDates)
		}
	}()
	budget := newCalculationBudget(ctx)
	c, e := readProjectCalendar(ctx, q, p, budget)
	if e != nil {
		invalidateSchedule(p, "blocked_capacity", e.Error())
		return e
	}
	people, e := readEmployees(ctx, q)
	if e != nil {
		return e
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	now := time.Now().In(loc)
	if e = calculate(p, c, people, now, budget); e != nil {
		return e
	}
	if p.CalculationState != "ready" || len(p.Loads) == 0 {
		return nil
	}
	totals := map[string]int{}
	limits := map[string]int{}
	own := map[string]Load{}
	employeeIDs := []string{}
	seenEmployees := map[string]bool{}
	for _, l := range p.Loads {
		key := l.EmployeeID + "/" + l.Date
		totals[key] += l.Minutes
		own[key] = l
		limits[key] = employeeDayLimit(people[l.EmployeeID], c.Days[l.Date])
		if !seenEmployees[l.EmployeeID] {
			employeeIDs = append(employeeIDs, l.EmployeeID)
			seenEmployees[l.EmployeeID] = true
		}
	}

	keys := []string{}
	for k := range own {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	blocked := func() bool {
		for _, k := range keys {
			if totals[k] > limits[k] {
				l := own[k]
				// Once a conflict is proven, further positive loads cannot repair it.
				// Report a lower bound, never claim this is the full global sum.
				invalidateSchedule(p, "blocked_capacity", fmt.Sprintf("Общий лимит · %s · %s: не менее %d мин при максимуме %d. Исправьте участие или задачи; расчёт остановлен.", people[l.EmployeeID].Name, l.Date, totals[k], limits[k]))
				return true
			}
		}
		return false
	}
	// Only projects with assignments to these employees can contribute actual work.
	rows, e := q.Query(ctx, `SELECT DISTINCT p.id FROM projects p JOIN assignments a ON a.project_id=p.id WHERE p.id<>$1 AND p.archived_at IS NULL AND a.employee_id=ANY($2::text[]) ORDER BY p.id`, p.ID, employeeIDs)
	if e != nil {
		return e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, id := range ids {
		other, err := readProject(ctx, q, id)
		if err != nil {
			return err
		}
		otherCal, err := readProjectCalendar(ctx, q, &other, budget)
		// Local calculation only: recursively calling prepare would double count or loop.
		if err == nil {
			err = calculate(&other, otherCal, people, now, budget)
		}
		if err != nil {
			var typed appError
			if errors.As(err, &typed) && typed.Code == "calculation_budget_exceeded" {
				invalidateSchedule(p, "blocked_capacity", typed.Message)
				return err
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				invalidateSchedule(p, "blocked_capacity", err.Error())
				return err
			}
			invalidateSchedule(p, "blocked_capacity", "Суммарная загрузка не подтверждена: есть нерассчитанный план для общего сотрудника")
			return nil
		}
		if other.CalculationState != "ready" {
			invalidateSchedule(p, "blocked_capacity", "Суммарная загрузка не подтверждена: настройте участие общего сотрудника в остальных проектах")
			return nil
		}
		for _, l := range other.Loads {
			key := l.EmployeeID + "/" + l.Date
			if _, ok := own[key]; !ok {
				continue
			}
			totals[key] += l.Minutes
			if limit := employeeDayLimit(people[l.EmployeeID], otherCal.Days[l.Date]); limit < limits[key] {
				limits[key] = limit
			}
		}
		if blocked() {
			return nil
		}
	}
	return nil
}
