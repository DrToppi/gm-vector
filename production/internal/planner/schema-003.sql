ALTER TABLE participation ADD COLUMN week jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(week) = 'object');
