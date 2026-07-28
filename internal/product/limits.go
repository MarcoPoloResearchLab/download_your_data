package product

const (
	// MaxArchiveUploadBytes is the maximum compressed OpenAI archive accepted by the local lifecycle.
	MaxArchiveUploadBytes int64 = 2 * 1024 * 1024 * 1024
	// MaxConversationEntryBytes is the maximum uncompressed recognized conversation payload.
	MaxConversationEntryBytes int64 = 1024 * 1024 * 1024
	// MaxArchiveEntryCount bounds archive directory traversal.
	MaxArchiveEntryCount = 10_000
	// MaxArchiveCompressionRatio bounds decompression amplification.
	MaxArchiveCompressionRatio = 100
	// MaxArchiveWorkingBytes bounds generation-owned working-disk use.
	MaxArchiveWorkingBytes int64 = 4 * 1024 * 1024 * 1024
	// DefaultInferenceBatchSize is the canonical local embedding batch size.
	DefaultInferenceBatchSize = 64
	// MaxTMDBQueryBytes bounds one derived title query.
	MaxTMDBQueryBytes = 512
	// MaxTMDBResponseBytes bounds every decoded TMDB response independently.
	MaxTMDBResponseBytes int64 = 2 * 1024 * 1024
	// MaxTMDBCacheResultBytes bounds one private serialized enrichment outcome.
	MaxTMDBCacheResultBytes = 256 * 1024
	// MaxTMDBSearchCandidates bounds one search result set.
	MaxTMDBSearchCandidates = 20
	// MaxTMDBConcurrency is the fixed title-enrichment worker count.
	MaxTMDBConcurrency = 4
	// TMDBRequestsPerSecond is the fixed client-wide request pace.
	TMDBRequestsPerSecond = 4
	// MaxTMDBAttempts includes the initial request and bounded retries.
	MaxTMDBAttempts = 3
	// MaxTMDBRetryAfterSeconds caps a server-directed retry delay.
	MaxTMDBRetryAfterSeconds = 30
	// TMDBCacheFreshDays is the fixed cache freshness window.
	TMDBCacheFreshDays = 30
)
