ALTER TABLE projects ADD COLUMN calendar_versions jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(calendar_versions) = 'object');
ALTER TABLE tasks DROP CONSTRAINT tasks_duration_days_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_duration_days_check CHECK (duration_days BETWEEN 0 AND 73414);
