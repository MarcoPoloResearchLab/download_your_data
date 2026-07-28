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
)
