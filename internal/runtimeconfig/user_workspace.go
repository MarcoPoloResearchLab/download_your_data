package runtimeconfig

import (
	"fmt"
	"path/filepath"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
)

// UserWorkspace contains every private path owned by one authenticated user.
type UserWorkspace struct {
	root             privatepath.Root
	archiveDatabase  privatepath.File
	netflixLibrary   privatepath.File
	netflixLease     privatepath.File
	netflixTMDBCache privatepath.File
}

func newUserWorkspace(root privatepath.Root) (UserWorkspace, error) {
	archiveDatabase, archiveError := root.File(
		filepath.FromSlash(product.ArchiveDatabaseRelativePath),
	)
	if archiveError != nil {
		return UserWorkspace{}, fmt.Errorf("resolve OpenAI archive database: %w", archiveError)
	}
	netflixLibrary, libraryError := root.File(
		filepath.FromSlash(product.NetflixLibraryStateRelativePath),
	)
	if libraryError != nil {
		return UserWorkspace{}, fmt.Errorf("resolve Netflix library state: %w", libraryError)
	}
	netflixLease, leaseError := root.File(
		filepath.FromSlash(product.NetflixLibraryLeaseRelativePath),
	)
	if leaseError != nil {
		return UserWorkspace{}, fmt.Errorf("resolve Netflix library lease: %w", leaseError)
	}
	netflixTMDBCache, cacheError := root.File(
		filepath.FromSlash(product.NetflixTMDBCacheRelativePath),
	)
	if cacheError != nil {
		return UserWorkspace{}, fmt.Errorf("resolve Netflix TMDB cache: %w", cacheError)
	}
	return UserWorkspace{
		root:             root,
		archiveDatabase:  archiveDatabase,
		netflixLibrary:   netflixLibrary,
		netflixLease:     netflixLease,
		netflixTMDBCache: netflixTMDBCache,
	}, nil
}

// Root returns the authenticated user's private filesystem root.
func (workspace UserWorkspace) Root() privatepath.Root {
	return workspace.root
}

// ArchiveDatabase returns this user's OpenAI database location.
func (workspace UserWorkspace) ArchiveDatabase() privatepath.File {
	return workspace.archiveDatabase
}

// NetflixLibrary returns this user's Netflix lifecycle repository.
func (workspace UserWorkspace) NetflixLibrary() privatepath.File {
	return workspace.netflixLibrary
}

// NetflixLease returns this user's Netflix cross-process lease.
func (workspace UserWorkspace) NetflixLease() privatepath.File {
	return workspace.netflixLease
}

// NetflixTMDBCache returns this user's Netflix metadata cache.
func (workspace UserWorkspace) NetflixTMDBCache() privatepath.File {
	return workspace.netflixTMDBCache
}
