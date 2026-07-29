// Package tmdb owns the sole remote metadata boundary for the Netflix provider.
package tmdb

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// ReadTokenEnvironment is the sole TMDB credential configuration key.
	ReadTokenEnvironment = "DOWNLOAD_YOUR_DATA_TMDB_READ_TOKEN"

	// OfficialBaseURL is the only production TMDB API origin.
	OfficialBaseURL = "https://api.themoviedb.org/3"

	// ClientIdentity changes whenever request or response semantics change.
	ClientIdentity = "tmdb-v3-bearer-client-v1"

	// DefaultLocale is the first canonical TMDB query locale.
	DefaultLocale = "en-US"

	// AttributionNotice is TMDB's required non-endorsement notice.
	AttributionNotice = "This product uses the TMDB API but is not endorsed or certified by TMDB."

	// AttributionWebsite is the required TMDB website link target.
	AttributionWebsite = "https://www.themoviedb.org"

	maxReadTokenBytes = 4096
)

var (
	// ErrNotConfigured identifies an absent TMDB read token.
	ErrNotConfigured = errors.New("TMDB read token is not configured")

	// ErrInvalidReadToken identifies a configured value that cannot be used safely.
	ErrInvalidReadToken = errors.New("invalid TMDB read token")

	// ErrInvalidLocale identifies a locale outside the current TMDB query contract.
	ErrInvalidLocale = errors.New("invalid TMDB locale")

	localePattern = regexp.MustCompile(`^[a-z]{2}-[A-Z]{2}$`)
)

// ReadToken is a validated server-only TMDB API Read Access Token.
type ReadToken struct {
	value string
}

// OptionalReadToken validates a configured token or returns not configured.
func OptionalReadToken(value string) (ReadToken, bool, error) {
	if value == "" {
		return ReadToken{}, false, nil
	}
	token, tokenError := NewReadToken(value)
	if tokenError != nil {
		return ReadToken{}, false, tokenError
	}
	return token, true, nil
}

// NewReadToken validates one server-only credential without exposing it in errors.
func NewReadToken(value string) (ReadToken, error) {
	if value == "" {
		return ReadToken{}, ErrNotConfigured
	}
	if len(value) > maxReadTokenBytes {
		return ReadToken{}, fmt.Errorf("%w: value is too long", ErrInvalidReadToken)
	}
	if strings.TrimSpace(value) != value || !utf8.ValidString(value) {
		return ReadToken{}, fmt.Errorf("%w: value must be trimmed UTF-8", ErrInvalidReadToken)
	}
	for _, character := range value {
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			return ReadToken{}, fmt.Errorf(
				"%w: whitespace and control characters are not allowed",
				ErrInvalidReadToken,
			)
		}
	}
	return ReadToken{value: value}, nil
}

func (token ReadToken) valid() bool {
	return token.value != ""
}

// Locale is a validated TMDB language and region identity.
type Locale struct {
	value string
}

// NewLocale validates the current ll-RR query locale contract.
func NewLocale(value string) (Locale, error) {
	if !localePattern.MatchString(value) {
		return Locale{}, fmt.Errorf("%w: expected ll-RR", ErrInvalidLocale)
	}
	return Locale{value: value}, nil
}

// String returns the normalized locale.
func (locale Locale) String() string {
	return locale.value
}

func (locale Locale) valid() bool {
	return localePattern.MatchString(locale.value)
}

// Attribution is the fixed Credits contract required before TMDB data is shown.
type Attribution struct {
	Name                 string `json:"name"`
	Website              string `json:"website"`
	Notice               string `json:"notice"`
	CreditsPlacement     bool   `json:"credits_placement"`
	ApprovedLogoRequired bool   `json:"approved_logo_required"`
	LogoModification     bool   `json:"logo_modification_allowed"`
}

// CreditsAttribution returns the current TMDB attribution requirements.
func CreditsAttribution() Attribution {
	return Attribution{
		Name:                 "TMDB",
		Website:              AttributionWebsite,
		Notice:               AttributionNotice,
		CreditsPlacement:     true,
		ApprovedLogoRequired: true,
		LogoModification:     false,
	}
}
