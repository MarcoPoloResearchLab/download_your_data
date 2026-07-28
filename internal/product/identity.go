// Package product owns canonical application identity values shared across commands and packages.
package product

const (
	// ArchiveCommandName is the current product-owned conversation archive executable.
	ArchiveCommandName = "download-your-data-archive"
	// DefaultArchiveDatabasePath is the archive command's default SQLite path.
	DefaultArchiveDatabasePath = "archive.db"
)
