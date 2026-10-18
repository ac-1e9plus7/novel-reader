UPDATE job_items SET status='invalidated',message='旧翻译任务已作废，请逐段创建新任务'
WHERE status!='succeeded' AND job_id IN (SELECT id FROM jobs WHERE kind='translation' AND status!='succeeded');
UPDATE jobs SET status='invalidated',cancel=1,message='旧翻译任务已作废，请逐段创建新任务'
WHERE kind='translation' AND status!='succeeded';
INSERT INTO schema_migrations VALUES(2);
