ALTER TABLE tasks ADD COLUMN customer_email text NOT NULL DEFAULT '' CHECK (length(customer_email)<=254 AND customer_email !~ '[\r\n]');
CREATE TABLE task_comments(
 id uuid PRIMARY KEY,
 project_id text NOT NULL,
 task_id text NOT NULL,
 author_id text NOT NULL,
 author_name text NOT NULL,
 body text NOT NULL CHECK(octet_length(body) BETWEEN 1 AND 8000),
 send_to_customer boolean NOT NULL DEFAULT false,
 recipient text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
);
CREATE INDEX task_comments_task_time ON task_comments(project_id,task_id,created_at,id);
CREATE INDEX outbox_customer_mail ON outbox(ready_at,id) WHERE provider='smtp' AND state='queued';
