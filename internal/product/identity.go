// Package product owns canonical application identity values shared across commands and packages.
package product

const (
	// CommandName is the sole product executable.
	CommandName = "download-your-data"
	// ArchiveDatabaseRelativePath is the sole conversation database location beneath the private data root.
	ArchiveDatabaseRelativePath = "openai/archive.db"
	// NetflixLibraryStateRelativePath is the sole provider lifecycle repository.
	NetflixLibraryStateRelativePath = "providers/netflix/library.json"
	// NetflixLibraryLeaseRelativePath is the sole cross-process mutation lease.
	NetflixLibraryLeaseRelativePath = "providers/netflix/library.lock"
	// NetflixTMDBCacheRelativePath is the sole private TMDB cache location.
	NetflixTMDBCacheRelativePath = "providers/netflix/tmdb-cache.db"
)
