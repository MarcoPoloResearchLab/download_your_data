package netflix

import (
	"errors"
	"regexp"
)

// IMDbIDSource identifies the sole accepted source of IMDb title IDs.
const IMDbIDSource = "tmdb-external-ids"

var imdbTitleIDPattern = regexp.MustCompile(`^tt[0-9]{7,}$`)

// IMDbTitleID is an immutable validated title ID. A nil value means absent.
// The sealed interface prevents callers from constructing an invalid zero value.
type IMDbTitleID interface {
	String() string
	imdbTitleID()
}

type imdbTitleID struct {
	value string
}

func (identifier imdbTitleID) String() string { return identifier.value }
func (identifier imdbTitleID) imdbTitleID()   {}

// NewIMDbTitleID validates a title identifier without including it in errors.
func NewIMDbTitleID(value string) (IMDbTitleID, error) {
	if len(value) > 32 || !imdbTitleIDPattern.MatchString(value) {
		return nil, errors.New("invalid IMDb title ID: expected tt followed by at least seven digits")
	}
	return imdbTitleID{value: value}, nil
}

// ParseIMDbTitleID validates the optional identifier and its source at an I/O edge.
func ParseIMDbTitleID(value, source string) (IMDbTitleID, error) {
	if value == "" && source == "" {
		return nil, nil
	}
	if value == "" || source != IMDbIDSource {
		return nil, errors.New("invalid IMDb title ID source")
	}
	return NewIMDbTitleID(value)
}
