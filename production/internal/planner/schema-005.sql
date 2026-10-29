ALTER TABLE employees ADD COLUMN email text NOT NULL DEFAULT '';
CREATE TABLE employee_invitations (
 id uuid PRIMARY KEY,
 employee_id text NOT NULL REFERENCES employees(id),
 user_id text NOT NULL REFERENCES users(id),
 actor text NOT NULL REFERENCES users(id),
 username text NOT NULL,
 email text NOT NULL,
 provider text NOT NULL,
 external_id text NOT NULL,
 token_hash text NOT NULL,
 reissue boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL,
 revoked boolean NOT NULL DEFAULT false
);
CREATE INDEX employee_invitations_employee ON employee_invitations(employee_id,created_at DESC);
CREATE INDEX outbox_invitation_mail ON outbox(ready_at,id) WHERE provider='smtp_invitation' AND state='queued';
