// Package product owns canonical application identity values shared across commands and packages.
package product

const (
	// ArchiveCommandName is the current product-owned conversation archive executable.
	ArchiveCommandName = "download-your-data-archive"
	// ArchiveDatabaseRelativePath is the sole conversation database location beneath the private data root.
	ArchiveDatabaseRelativePath = "openai/archive.db"
	// NetflixLibraryStateRelativePath is the sole provider lifecycle repository.
	NetflixLibraryStateRelativePath = "providers/netflix/library.json"
	// NetflixLibraryLeaseRelativePath is the sole cross-process mutation lease.
	NetflixLibraryLeaseRelativePath = "providers/netflix/library.lock"
	// NetflixTMDBCacheRelativePath is the sole private TMDB cache location.
	NetflixTMDBCacheRelativePath = "providers/netflix/tmdb-cache.db"
)
