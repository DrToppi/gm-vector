package planner

import (
	"context"
	"encoding/json"
	"testing"
)

func runCommentIntegration(t *testing.T, a *App, request func(string, string, any, *testSession, *string) (int, json.RawMessage), admin *testSession) {
	t.Helper()
	ctx := context.Background()
	t.Run("customer comment durable opt in", func(t *testing.T) {
		call := func(want int, method, path string, body any) json.RawMessage {
			t.Helper()
			status, b := request(method, path, body, admin, nil)
			if status != want {
				t.Fatalf("%s %s: %d want %d: %s", method, path, status, want, b)
			}
			return b
		}
		var p Project
		json.Unmarshal(call(201, "POST", "/projects", map[string]any{"title": "Customer mail test", "start": "2026-10-23", "calendar_id": "test", "duration_mode": "working"}), &p)
		if p.ID == "" {
			t.Fatal("missing project")
		}
		path := "/projects/" + p.ID
		p.Tasks = []Task{{ID: "customer-task", Title: "Customer work", Responsibility: "customer", Status: "todo", Start: "2026-10-23", End: "2026-10-23", CustomerEmail: "customer@example.test"}}
		save := func() { t.Helper(); json.Unmarshal(call(200, "PUT", path, saveRequest{Project: p}), &p) }
		save()
		comments := path + "/tasks/customer-task/comments"
		var list struct {
			Comments      []TaskComment
			EmailEnabled  bool   `json:"email_enabled"`
			CanComment    bool   `json:"can_comment"`
			CustomerEmail string `json:"customer_email"`
		}
		json.Unmarshal(call(200, "GET", comments, nil), &list)
		if list.EmailEnabled || !list.CanComment || list.CustomerEmail != "customer@example.test" {
			t.Fatal("wrong saved comment capability")
		}
		local := map[string]any{"id": "10000000-0000-4000-8000-000000000001", "text": "Local note", "send_to_customer": false}
		var c TaskComment
		json.Unmarshal(call(201, "POST", comments, local), &c)
		if c.AuthorID == "" || c.AuthorName == "" || c.DeliveryState != "internal" || c.Recipient != "" {
			t.Fatal("unsafe local comment")
		}
		call(200, "POST", comments, local)
		local["text"] = "Changed"
		call(409, "POST", comments, local)
		external := map[string]any{"id": "10000000-0000-4000-8000-000000000002", "text": "Explicit customer note", "send_to_customer": true}
		call(503, "POST", comments, external)
		var n int
		if e := a.Store.DB.QueryRow(ctx, "SELECT count(*) FROM task_comments WHERE id=$1", external["id"]).Scan(&n); e != nil || n != 0 {
			t.Fatal("disabled send stored intent", e)
		}

		// New immutable comments are limited to managers and assigned members.
		memberID := newID()
		sealed, _ := seal(a.Config.Secret, []byte(totpSecret()))
		member := testSession{token(), token()}
		exec := func(sql string, args ...any) {
			t.Helper()
			if _, e := a.Store.DB.Exec(ctx, sql, args...); e != nil {
				t.Fatal(e)
			}
		}
		exec("INSERT INTO users(id,username,name,password_hash,totp,active) VALUES($1,$2,'Comment member','unused',$3,true)", memberID, "comment-"+memberID, sealed)
		exec("INSERT INTO sessions(hash,user_id,csrf) VALUES($1,$2,$3)", hash(member.cookie), memberID, member.csrf)
		exec("INSERT INTO memberships(project_id,user_id,role) VALUES($1,$2,'member')", p.ID, memberID)
		memberComment := map[string]any{"id": "10000000-0000-4000-8000-000000000004", "text": "Assigned member comment", "send_to_customer": false}
		status, _ := request("POST", comments, memberComment, &member, nil)
		if status != 403 {
			t.Fatal("unassigned member comment accepted")
		}
		exec("INSERT INTO employees(id,user_id,name,daily_minutes) VALUES($1,$2,'Comment member',480)", memberID, memberID)
		exec("INSERT INTO assignments(project_id,task_id,employee_id,daily_minutes) VALUES($1,'customer-task',$2,60)", p.ID, memberID)
		status, _ = request("POST", comments, memberComment, &member, nil)
		if status != 201 {
			t.Fatalf("assigned member comment denied: %d", status)
		}
		exec("UPDATE memberships SET role='viewer' WHERE project_id=$1 AND user_id=$2", p.ID, memberID)
		status, _ = request("POST", comments, memberComment, &member, nil)
		if status != 403 {
			t.Fatal("viewer comment accepted")
		}
		status, _ = request("GET", comments, nil, &member, nil)
		if status != 200 {
			t.Fatal("viewer cannot read discussion")
		}
		exec("DELETE FROM assignments WHERE project_id=$1 AND employee_id=$2", p.ID, memberID)
		wrong := "wrong"
		status, _ = request("POST", comments, local, admin, &wrong)
		if status != 403 {
			t.Fatal("comment CSRF not enforced")
		}
		// A separate handler config enables queueing, with no SMTP network calls here.
		a.Config.SMTP = SMTPConfig{Enabled: true, Host: "localhost", Port: 1, Security: "tls", From: "planner@example.test"}
		defer func() { a.Config.SMTP = SMTPConfig{} }()
		json.Unmarshal(call(201, "POST", comments, external), &c)
		if c.DeliveryState != "queued" || c.Recipient != "customer@example.test" {
			t.Fatal("missing queued snapshot")
		}
		call(200, "POST", comments, external)
		if e := a.Store.DB.QueryRow(ctx, "SELECT count(*) FROM outbox WHERE id=$1", c.ID).Scan(&n); e != nil || n != 1 {
			t.Fatal("duplicate intent", e)
		}
		p.Tasks[0].CustomerEmail = "second@example.test"
		save()
		got, e := readComment(ctx, a.Store.DB, c.ID)
		if e != nil || got.DeliveryState != "failed" || got.Recipient != "customer@example.test" {
			t.Fatal("recipient change did not cancel snapshot", e)
		}
		p.Tasks[0].CustomerEmail = "customer@example.test"
		save()
		got, e = readComment(ctx, a.Store.DB, c.ID)
		if e != nil || got.DeliveryState != "failed" {
			t.Fatal("cancelled intention resurrected")
		}

		restoredTasks := append([]Task(nil), p.Tasks...)
		p.Tasks = []Task{}
		save()
		call(404, "GET", comments, nil)
		p.Tasks = restoredTasks
		save()
		json.Unmarshal(call(200, "GET", comments, nil), &list)
		if len(list.Comments) != 3 {
			t.Fatalf("undo-style restoration lost discussion: %d", len(list.Comments))
		}
		p.Tasks[0].CustomerEmail = "Name <bad@example.test>"
		call(422, "PUT", path, saveRequest{Project: p})
		p.Tasks[0].CustomerEmail = "customer@example.test"
		external["id"] = "10000000-0000-4000-8000-000000000003"
		call(201, "POST", comments, external)
		call(200, "DELETE", path, map[string]any{"revision": p.Revision})
		got, e = readComment(ctx, a.Store.DB, external["id"].(string))
		if e != nil || got.DeliveryState != "failed" {
			t.Fatal("archive did not cancel")
		}
		if _, e = a.Store.DB.Exec(ctx, "UPDATE outbox SET state='sending',leased_until=now()-interval '1 second' WHERE id=$1", external["id"]); e != nil {
			t.Fatal(e)
		}
		a.Config.SMTP = SMTPConfig{}
		if e = a.customerMailTick(ctx); e != nil {
			t.Fatal(e)
		}
		got, e = readComment(ctx, a.Store.DB, external["id"].(string))
		if e != nil || got.DeliveryState != "uncertain" {
			t.Fatal("stale sending must not retry")
		}
	})
}
