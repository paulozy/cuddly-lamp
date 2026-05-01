CREATE INDEX IF NOT EXISTS idx_code_analyses_repo_pr_type_created
ON code_analyses (repository_id, pull_request_id, type, created_at DESC)
WHERE pull_request_id IS NOT NULL;
