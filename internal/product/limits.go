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
	// MaxNetflixViewingCSVBytes bounds one uncompressed viewing-activity upload.
	MaxNetflixViewingCSVBytes int64 = 64 * 1024 * 1024
	// MaxNetflixViewingRows bounds one local generation.
	MaxNetflixViewingRows = 250_000
	// MaxNetflixUniqueTitles bounds derived identities in one generation.
	MaxNetflixUniqueTitles = 100_000
	// MaxNetflixTitleBytes bounds one raw or derived title.
	MaxNetflixTitleBytes = 8 * 1024
	// MaxNetflixFieldBytes bounds every non-title CSV field.
	MaxNetflixFieldBytes = 16 * 1024
	// MaxNetflixWorkingBytes bounds one generation's source and built artifacts.
	MaxNetflixWorkingBytes int64 = 512 * 1024 * 1024
	// MaxNetflixEnrichmentOutcomeBytes bounds one private title checkpoint.
	MaxNetflixEnrichmentOutcomeBytes int64 = 320 * 1024
	// MaxNetflixProgressEvents bounds the persisted event journal per generation.
	MaxNetflixProgressEvents = 256
	// MaxNetflixGenerationHistory bounds the persisted provider generation journal.
	MaxNetflixGenerationHistory = 256
	// MaxNetflixConcurrentBuilds is the sole provider build concurrency.
	MaxNetflixConcurrentBuilds = 1
	// MaxNetflixRecordPageSize bounds one records response.
	MaxNetflixRecordPageSize = 200
	// DefaultNetflixRecordPageSize is the canonical first records page size.
	DefaultNetflixRecordPageSize = 50
	// MaxNetflixJSONRequestBytes bounds lifecycle mutation payloads.
	MaxNetflixJSONRequestBytes int64 = 4 * 1024
	// MinNetflixViewingYear is Netflix's launch year and the earliest accepted viewing date.
	MinNetflixViewingYear = 1997
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
