package planner

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var commentUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func validEmail(value string) bool {
	if len(value) == 0 || len(value) > 254 || strings.ContainsAny(value, "\r\n\t ,;<>\"()") {
		return false
	}
	for _, r := range value {
		if r < 33 || r > 126 {
			return false
		}
	}
	a, e := mail.ParseAddress(value)
	return e == nil && a.Name == "" && a.Address == value && strings.Count(value, "@") == 1
}

func canComment(ctx context.Context, q DBTX, u Principal, role, project, task string) bool {
	if role == "manager" {
		return true
	}
	if role != "member" || u.EmployeeID == "" {
		return false
	}
	var assigned bool
	e := q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM assignments WHERE project_id=$1 AND task_id=$2 AND employee_id=$3)", project, task, u.EmployeeID).Scan(&assigned)
	return e == nil && assigned
}

const commentColumns = `c.id::text,c.project_id,c.task_id,c.author_id,c.author_name,c.body,c.created_at,c.send_to_customer,c.recipient,coalesce(o.state,'internal'),coalesce(o.last_error,'')`

func scanComment(row pgx.Row) (TaskComment, error) {
	var c TaskComment
	var created time.Time
	e := row.Scan(&c.ID, &c.ProjectID, &c.TaskID, &c.AuthorID, &c.AuthorName, &c.Text, &created, &c.SendToCustomer, &c.Recipient, &c.DeliveryState, &c.DeliveryError)
	c.CreatedAt = created.UTC().Format(time.RFC3339Nano)
	if c.DeliveryState == "sending" {
		c.DeliveryState = "queued"
	}
	return c, e
}
func readComment(ctx context.Context, q DBTX, id string) (TaskComment, error) {
	return scanComment(q.QueryRow(ctx, "SELECT "+commentColumns+" FROM task_comments c LEFT JOIN outbox o ON o.id=c.id::text AND o.provider='smtp' WHERE c.id=$1", id))
}
func (a *App) taskComments(w http.ResponseWriter, r *http.Request, u Principal) error {
	role, e := a.requireProject(r, u, false)
	if e != nil {
		return e
	}
	project, task := r.PathValue("id"), r.PathValue("task")
	var responsibility, email string
	if e = a.Store.DB.QueryRow(r.Context(), "SELECT responsibility,customer_email FROM tasks WHERE project_id=$1 AND id=$2", project, task).Scan(&responsibility, &email); e != nil {
		return e
	}
	rows, e := a.Store.DB.Query(r.Context(), "SELECT "+commentColumns+" FROM task_comments c LEFT JOIN outbox o ON o.id=c.id::text AND o.provider='smtp' WHERE c.project_id=$1 AND c.task_id=$2 ORDER BY c.created_at,c.id", project, task)
	if e != nil {
		return e
	}
	out := []TaskComment{}
	for rows.Next() {
		c, e := scanComment(rows)
		if e != nil {
			rows.Close()
			return e
		}
		out = append(out, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	jsonResponse(w, 200, map[string]any{"comments": out, "email_enabled": a.Config.SMTP.Enabled, "can_comment": (!u.Token || contains(u.Scopes, "project:write")) && canComment(r.Context(), a.Store.DB, u, role, project, task), "customer_email": email, "customer": responsibility == "customer"})
	return nil
}
func (a *App) addTaskComment(w http.ResponseWriter, r *http.Request, u Principal) error {
	if _, e := a.requireProject(r, u, true); e != nil {
		return e
	}
	var in struct {
		ID   string `json:"id"`
		Text string `json:"text"`
		Send bool   `json:"send_to_customer"`
	}
	if e := decode(r, &in); e != nil {
		return e
	}
	if !commentUUID.MatchString(in.ID) || !utf8.ValidString(in.Text) || len(in.Text) > 8000 || strings.TrimSpace(in.Text) == "" {
		return bad("Нужны UUID комментария и текст от 1 до 8000 байт")
	}
	in.ID = strings.ToLower(in.ID)
	ctx := r.Context()
	tx, e := a.Store.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	project, task := r.PathValue("id"), r.PathValue("task")
	var projectID string
	if e = tx.QueryRow(ctx, "SELECT id FROM projects WHERE id=$1 AND archived_at IS NULL FOR UPDATE", project).Scan(&projectID); e != nil {
		return e
	}
	role, e := a.lockedRole(ctx, tx, u, project)
	if e != nil {
		return e
	}
	if !canComment(ctx, tx, u, role, project, task) {
		return denied()
	}
	var responsibility, email string
	if e = tx.QueryRow(ctx, "SELECT responsibility,customer_email FROM tasks WHERE project_id=$1 AND id=$2", project, task).Scan(&responsibility, &email); e != nil {
		return e
	}
	// Project row serializes normal retries; unique UUID also rejects cross-project collisions.
	previous, e := readComment(ctx, tx, in.ID)
	if e == nil {
		if previous.ProjectID != project || previous.TaskID != task || previous.AuthorID != u.ID || previous.Text != in.Text || previous.SendToCustomer != in.Send {
			return conflict()
		}
		jsonResponse(w, 200, previous)
		return nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	recipient := ""
	if in.Send {
		if responsibility != "customer" || !validEmail(email) {
			return bad("Отправка доступна только задаче заказчика с корректной почтой")
		}
		if !a.Config.SMTP.Enabled {
			return appError{503, "email_disabled", "Почтовая отправка пока не настроена; сохраните локальный комментарий"}
		}
		recipient = email
	}
	tag, e := tx.Exec(ctx, `INSERT INTO task_comments(id,project_id,task_id,author_id,author_name,body,send_to_customer,recipient) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(id) DO NOTHING`, in.ID, project, task, u.ID, u.Name, in.Text, in.Send, recipient)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return conflict()
	}
	if in.Send {
		if _, e = tx.Exec(ctx, "INSERT INTO outbox(id,project_id,provider,payload,state) VALUES($1,$2,'smtp','{}','queued')", in.ID, project); e != nil {
			return e
		}
	}
	if e = audit(ctx, tx, u.ID, project, "task.comment_created", requestID(r), map[string]any{"comment_id": in.ID, "task_id": task, "send_to_customer": in.Send}); e != nil {
		return e
	}
	result, e := readComment(ctx, tx, in.ID)
	if e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	jsonResponse(w, 201, result)
	return nil
}

// Call while holding the project row lock. A changed recipient cannot be restored
// later to resurrect an already cancelled intention, and a deleted task can't send.
func cancelInvalidCustomerMail(ctx context.Context, q DBTX, project string) error {
	_, e := q.Exec(ctx, `UPDATE outbox o SET state='failed',last_error='recipient_or_task_changed',leased_until=NULL WHERE o.project_id=$1 AND o.provider='smtp' AND o.state IN ('queued','sending') AND NOT EXISTS(SELECT 1 FROM task_comments c JOIN tasks t ON t.project_id=c.project_id AND t.id=c.task_id JOIN projects p ON p.id=c.project_id WHERE c.id::text=o.id AND p.archived_at IS NULL AND t.responsibility='customer' AND t.customer_email=c.recipient)`, project)
	return e
}
