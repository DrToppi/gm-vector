ALTER TABLE users ADD COLUMN locale text NOT NULL DEFAULT 'ru' CHECK(locale IN ('en','ru','de','fr','es','pt-BR','zh-Hans'));
