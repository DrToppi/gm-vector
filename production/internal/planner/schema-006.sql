ALTER TABLE users ADD COLUMN auth_provider text NOT NULL DEFAULT 'local';
ALTER TABLE users ADD COLUMN directory_id text NOT NULL DEFAULT '';
CREATE TABLE directory_entries (
 id text PRIMARY KEY,
 provider text NOT NULL,
 external_id text NOT NULL,
 name text NOT NULL,
 email text NOT NULL DEFAULT '',
 username text NOT NULL DEFAULT '',
 active boolean NOT NULL,
 groups text[] NOT NULL DEFAULT '{}',
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(provider,external_id)
);
CREATE INDEX directory_source ON directory_entries(provider,external_id);
