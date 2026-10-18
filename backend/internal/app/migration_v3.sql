CREATE TABLE app_settings(key TEXT PRIMARY KEY,value INTEGER NOT NULL);
CREATE TABLE translation_batches(id TEXT PRIMARY KEY,chapter_id TEXT NOT NULL REFERENCES chapters(id) ON DELETE CASCADE,revision INTEGER NOT NULL,created TEXT NOT NULL);
CREATE TABLE translation_batch_jobs(batch_id TEXT NOT NULL REFERENCES translation_batches(id) ON DELETE CASCADE,job_id TEXT NOT NULL UNIQUE REFERENCES jobs(id) ON DELETE CASCADE,PRIMARY KEY(batch_id,job_id));
CREATE INDEX translation_batches_chapter ON translation_batches(chapter_id,created);
INSERT INTO schema_migrations VALUES(3);
