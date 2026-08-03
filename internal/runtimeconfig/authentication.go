package runtimeconfig

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tyemirov/tauth/pkg/sessionvalidator"
)

const (
	PublicOriginEnvironment       = "DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN"
	APIOriginEnvironment          = "DOWNLOAD_YOUR_DATA_API_ORIGIN"
	TAuthURLEnvironment           = "DOWNLOAD_YOUR_DATA_TAUTH_URL"
	TAuthTenantIDEnvironment      = "DOWNLOAD_YOUR_DATA_TAUTH_TENANT_ID"
	TAuthJWTSigningKeyEnvironment = "DOWNLOAD_YOUR_DATA_TAUTH_JWT_SIGNING_KEY"
	TAuthSessionCookieEnvironment = "DOWNLOAD_YOUR_DATA_TAUTH_SESSION_COOKIE_NAME"
	TAuthRefreshCookieEnvironment = "DOWNLOAD_YOUR_DATA_TAUTH_REFRESH_COOKIE_NAME"
	GoogleClientIDEnvironment     = "DOWNLOAD_YOUR_DATA_GOOGLE_CLIENT_ID"
	TAuthJWTIssuer                = "tauth"
	TAuthLoginPath                = "/auth/google"
	TAuthLogoutPath               = "/auth/logout"
	TAuthNoncePath                = "/auth/nonce"
	TAuthSessionPath              = "/auth/session"
	TAuthRefreshPath              = "/auth/refresh"
	minimumTAuthSigningKeyBytes   = 32
)

// Authentication is the complete validated browser and backend TAuth
// configuration. Secret values are available only through the server-side
// validator configuration.
type Authentication struct {
	publicOrigin      string
	apiOrigin         string
	tAuthURL          string
	tAuthTenantID     string
	tAuthSigningKey   string
	sessionCookieName string
	refreshCookieName string
	googleClientID    string
}

// PublicOrigin returns the sole allowed static browser origin.
func (authentication Authentication) PublicOrigin() string {
	return authentication.publicOrigin
}

// APIOrigin returns the browser-facing protected API origin.
func (authentication Authentication) APIOrigin() string {
	return authentication.apiOrigin
}

// TAuthURL returns the browser-facing TAuth origin.
func (authentication Authentication) TAuthURL() string {
	return authentication.tAuthURL
}

// TenantID returns the sole accepted TAuth tenant.
func (authentication Authentication) TenantID() string {
	return authentication.tAuthTenantID
}

// SessionCookieName returns the exact TAuth session cookie name.
func (authentication Authentication) SessionCookieName() string {
	return authentication.sessionCookieName
}

// RefreshCookieName returns the exact TAuth refresh cookie name.
func (authentication Authentication) RefreshCookieName() string {
	return authentication.refreshCookieName
}

// GoogleClientID returns the browser-safe Google OAuth client ID.
func (authentication Authentication) GoogleClientID() string {
	return authentication.googleClientID
}

// SessionValidatorConfig returns the exact server-only TAuth validator
// configuration.
func (authentication Authentication) SessionValidatorConfig() sessionvalidator.Config {
	return sessionvalidator.Config{
		SigningKey: []byte(authentication.tAuthSigningKey),
		Issuer:     TAuthJWTIssuer,
		CookieName: authentication.sessionCookieName,
	}
}

func loadAuthentication(
	lookupEnvironment func(string) string,
) (Authentication, error) {
	publicOrigin, publicOriginError := newRequiredOrigin(
		PublicOriginEnvironment,
		lookupEnvironment(PublicOriginEnvironment),
	)
	if publicOriginError != nil {
		return Authentication{}, publicOriginError
	}
	apiOrigin, apiOriginError := newRequiredOrigin(
		APIOriginEnvironment,
		lookupEnvironment(APIOriginEnvironment),
	)
	if apiOriginError != nil {
		return Authentication{}, apiOriginError
	}
	tAuthURL, tAuthURLError := newRequiredOrigin(
		TAuthURLEnvironment,
		lookupEnvironment(TAuthURLEnvironment),
	)
	if tAuthURLError != nil {
		return Authentication{}, tAuthURLError
	}
	tenantID, tenantError := newRequiredConfigurationValue(
		TAuthTenantIDEnvironment,
		lookupEnvironment(TAuthTenantIDEnvironment),
	)
	if tenantError != nil {
		return Authentication{}, tenantError
	}
	signingKey, signingKeyError := newRequiredConfigurationValue(
		TAuthJWTSigningKeyEnvironment,
		lookupEnvironment(TAuthJWTSigningKeyEnvironment),
	)
	if signingKeyError != nil {
		return Authentication{}, signingKeyError
	}
	if len([]byte(signingKey)) < minimumTAuthSigningKeyBytes {
		return Authentication{}, fmt.Errorf(
			"validate %s: value must contain at least %d bytes",
			TAuthJWTSigningKeyEnvironment,
			minimumTAuthSigningKeyBytes,
		)
	}
	sessionCookieName, sessionCookieError := newCookieName(
		TAuthSessionCookieEnvironment,
		lookupEnvironment(TAuthSessionCookieEnvironment),
	)
	if sessionCookieError != nil {
		return Authentication{}, sessionCookieError
	}
	refreshCookieName, refreshCookieError := newCookieName(
		TAuthRefreshCookieEnvironment,
		lookupEnvironment(TAuthRefreshCookieEnvironment),
	)
	if refreshCookieError != nil {
		return Authentication{}, refreshCookieError
	}
	if refreshCookieName == sessionCookieName {
		return Authentication{}, fmt.Errorf(
			"validate %s: refresh and session cookie names must differ",
			TAuthRefreshCookieEnvironment,
		)
	}
	googleClientID, clientIDError := newRequiredConfigurationValue(
		GoogleClientIDEnvironment,
		lookupEnvironment(GoogleClientIDEnvironment),
	)
	if clientIDError != nil {
		return Authentication{}, clientIDError
	}
	return Authentication{
		publicOrigin:      publicOrigin,
		apiOrigin:         apiOrigin,
		tAuthURL:          tAuthURL,
		tAuthTenantID:     tenantID,
		tAuthSigningKey:   signingKey,
		sessionCookieName: sessionCookieName,
		refreshCookieName: refreshCookieName,
		googleClientID:    googleClientID,
	}, nil
}

func newRequiredOrigin(label string, value string) (string, error) {
	validatedValue, valueError := newRequiredConfigurationValue(label, value)
	if valueError != nil {
		return "", valueError
	}
	parsedOrigin, parseError := url.Parse(validatedValue)
	if parseError != nil ||
		parsedOrigin.User != nil ||
		parsedOrigin.Host == "" ||
		parsedOrigin.RawQuery != "" ||
		parsedOrigin.Fragment != "" ||
		(parsedOrigin.Path != "" && parsedOrigin.Path != "/") {
		return "", fmt.Errorf(
			"validate %s: value must be an HTTP origin without credentials, path, query, or fragment",
			label,
		)
	}
	if parsedOrigin.Scheme != "https" {
		hostAddress := net.ParseIP(parsedOrigin.Hostname())
		localhost := strings.EqualFold(parsedOrigin.Hostname(), "localhost")
		if parsedOrigin.Scheme != "http" ||
			(!localhost && (hostAddress == nil || !hostAddress.IsLoopback())) {
			return "", fmt.Errorf(
				"validate %s: hosted origins require HTTPS and HTTP is allowed only for loopback",
				label,
			)
		}
	}
	if parsedOrigin.Port() != "" {
		if _, portError := net.LookupPort("tcp", parsedOrigin.Port()); portError != nil {
			return "", fmt.Errorf("validate %s: invalid origin port", label)
		}
	}
	parsedOrigin.Path = ""
	return parsedOrigin.String(), nil
}

func newRequiredConfigurationValue(label string, value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("validate %s: value is required", label)
	}
	if value != strings.TrimSpace(value) {
		return "", fmt.Errorf("validate %s: surrounding whitespace is not allowed", label)
	}
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("validate %s: value must be UTF-8", label)
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", fmt.Errorf("validate %s: control characters are not allowed", label)
		}
	}
	return value, nil
}

func newCookieName(label string, value string) (string, error) {
	cookieName, valueError := newRequiredConfigurationValue(label, value)
	if valueError != nil {
		return "", valueError
	}
	if !validCookieName(cookieName) {
		return "", fmt.Errorf("validate %s: value must be an HTTP cookie token", label)
	}
	return cookieName, nil
}

func validCookieName(name string) bool {
	if name == "" {
		return false
	}
	for characterIndex := 0; characterIndex < len(name); characterIndex++ {
		character := name[characterIndex]
		if character <= 0x20 ||
			character >= 0x7f ||
			strings.ContainsRune(`()<>@,;:\"/[]?={} `, rune(character)) {
			return false
		}
	}
	return true
}
