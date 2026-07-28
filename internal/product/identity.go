// Package product owns canonical application identity values shared across commands and packages.
package product

const (
	// ArchiveCommandName is the current product-owned conversation archive executable.
	ArchiveCommandName = "download-your-data-archive"
	// ArchiveDatabaseRelativePath is the sole conversation database location beneath the private data root.
	ArchiveDatabaseRelativePath = "openai/archive.db"
	// NetflixTMDBCacheRelativePath is the sole private TMDB cache location.
	NetflixTMDBCacheRelativePath = "providers/netflix/tmdb-cache.db"
)
