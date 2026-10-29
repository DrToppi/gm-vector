ALTER TABLE tasks ADD COLUMN duration_days integer NOT NULL DEFAULT 0 CHECK (duration_days BETWEEN 0 AND 730);
