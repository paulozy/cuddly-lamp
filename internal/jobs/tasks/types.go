package tasks

// Task type constants used by both the enqueuer and the worker handler registry.
// Add new constants here as features are implemented.
const (
	TypeAnalyzeRepo        = "repo:analyze"
	TypeProcessWebhook     = "webhook:process"
	TypeGenerateEmbeddings = "embeddings:generate"
)
