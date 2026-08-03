// Package productionprofile owns the exact public production topology.
package productionprofile

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/uiconfig"
	"gopkg.in/yaml.v3"
)

const (
	currentSchemaVersion = 1
	maximumProfileBytes  = 16 * 1024
)

var cookieNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Profile is the complete non-secret production topology.
type Profile struct {
	SchemaVersion int     `yaml:"schema_version"`
	Browser       Browser `yaml:"browser"`
	Session       Session `yaml:"session"`
	CORS          CORS    `yaml:"cors"`
	Runtime       Runtime `yaml:"runtime"`
}

// Browser contains the public frontend, API, and shared-auth literals.
type Browser struct {
	PublicOrigin      string `yaml:"public_origin"`
	APIOrigin         string `yaml:"api_origin"`
	TAuthOrigin       string `yaml:"tauth_origin"`
	TenantID          string `yaml:"tenant_id"`
	GoogleWebClientID string `yaml:"google_web_client_id"`
	LoginPath         string `yaml:"login_path"`
	LogoutPath        string `yaml:"logout_path"`
	NoncePath         string `yaml:"nonce_path"`
	SessionPath       string `yaml:"session_path"`
}

// Session contains the hosted TAuth cookie policy.
type Session struct {
	CookieDomain      string `yaml:"cookie_domain"`
	SessionCookieName string `yaml:"session_cookie_name"`
	RefreshCookieName string `yaml:"refresh_cookie_name"`
	Secure            bool   `yaml:"secure"`
	SameSite          string `yaml:"same_site"`
}

// CORS contains the sole credentialed browser origin.
type CORS struct {
	AllowedOrigin string `yaml:"allowed_origin"`
	Credentials   bool   `yaml:"credentials"`
}

// Runtime contains the container boundary exposed through Caddy.
type Runtime struct {
	ContainerPort int    `yaml:"container_port"`
	HealthPath    string `yaml:"health_path"`
	DataMount     string `yaml:"data_mount"`
}

// Load decodes and validates one exact production profile from disk.
func Load(path string) (Profile, error) {
	profileFile, openError := os.Open(path)
	if openError != nil {
		return Profile{}, fmt.Errorf("open production profile %s: %w", path, openError)
	}
	defer profileFile.Close()

	encodedProfile, readError := io.ReadAll(io.LimitReader(profileFile, maximumProfileBytes+1))
	if readError != nil {
		return Profile{}, fmt.Errorf("read production profile %s: %w", path, readError)
	}
	if len(encodedProfile) > maximumProfileBytes {
		return Profile{}, fmt.Errorf(
			"read production profile %s: profile exceeds %d bytes",
			path,
			maximumProfileBytes,
		)
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(encodedProfile)))
	decoder.KnownFields(true)
	var profile Profile
	if decodeError := decoder.Decode(&profile); decodeError != nil {
		return Profile{}, fmt.Errorf("decode production profile %s: %w", path, decodeError)
	}
	if trailingError := decoder.Decode(&struct{}{}); !errors.Is(trailingError, io.EOF) {
		return Profile{}, fmt.Errorf("decode production profile %s: trailing YAML document is not allowed", path)
	}
	if validationError := profile.Validate(); validationError != nil {
		return Profile{}, fmt.Errorf("validate production profile %s: %w", path, validationError)
	}
	return profile, nil
}

// Validate proves the split-origin browser, auth, and runtime invariants.
func (profile Profile) Validate() error {
	if profile.SchemaVersion != currentSchemaVersion {
		return fmt.Errorf("schema version = %d; want %d", profile.SchemaVersion, currentSchemaVersion)
	}
	if profile.Browser.PublicOrigin == profile.Browser.APIOrigin {
		return errors.New("frontend and API origins must be distinct")
	}
	if profile.Browser.TAuthOrigin != profile.Browser.APIOrigin {
		return errors.New("browser TAuth origin must use the API Caddy front door")
	}
	if _, renderError := uiconfig.Render(profile.UIConfigInput()); renderError != nil {
		return renderError
	}
	if profile.CORS.AllowedOrigin != profile.Browser.PublicOrigin || !profile.CORS.Credentials {
		return errors.New("credentialed CORS must name the exact frontend origin")
	}
	if !profile.Session.Secure || profile.Session.SameSite != "None" {
		return errors.New("hosted TAuth cookies must be Secure with SameSite=None")
	}
	if profile.Session.SessionCookieName == profile.Session.RefreshCookieName ||
		!validCookieName(profile.Session.SessionCookieName) ||
		!validCookieName(profile.Session.RefreshCookieName) {
		return errors.New("session and refresh cookies must be distinct HTTP cookie tokens")
	}
	if domainError := validateCookieDomain(profile.Session.CookieDomain, profile.Browser); domainError != nil {
		return domainError
	}
	if profile.Runtime.ContainerPort < 1 || profile.Runtime.ContainerPort > 65535 {
		return errors.New("runtime container port must be between 1 and 65535")
	}
	if pathError := validateRuntimePath("health path", profile.Runtime.HealthPath); pathError != nil {
		return pathError
	}
	if !filepath.IsAbs(profile.Runtime.DataMount) ||
		profile.Runtime.DataMount == string(filepath.Separator) ||
		filepath.Clean(profile.Runtime.DataMount) != profile.Runtime.DataMount {
		return errors.New("runtime data mount must be one normalized absolute path")
	}
	return nil
}

// UIConfigInput projects the production profile into the public mpr-ui config.
func (profile Profile) UIConfigInput() uiconfig.Input {
	return uiconfig.Input{
		Description:       "Download Your Data",
		PublicOrigin:      profile.Browser.PublicOrigin,
		TAuthOrigin:       profile.Browser.TAuthOrigin,
		GoogleWebClientID: profile.Browser.GoogleWebClientID,
		TenantID:          profile.Browser.TenantID,
		LoginPath:         profile.Browser.LoginPath,
		LogoutPath:        profile.Browser.LogoutPath,
		NoncePath:         profile.Browser.NoncePath,
		SessionPath:       profile.Browser.SessionPath,
	}
}

func validateCookieDomain(cookieDomain string, browser Browser) error {
	if !strings.HasPrefix(cookieDomain, ".") || strings.Count(cookieDomain, ".") < 2 {
		return errors.New("cookie domain must be one explicit leading-dot parent domain")
	}
	domain := strings.TrimPrefix(cookieDomain, ".")
	for label, origin := range map[string]string{
		"frontend": browser.PublicOrigin,
		"API":      browser.APIOrigin,
		"TAuth":    browser.TAuthOrigin,
	} {
		parsedOrigin, parseError := url.Parse(origin)
		if parseError != nil || !strings.HasSuffix(parsedOrigin.Hostname(), "."+domain) {
			return fmt.Errorf("cookie domain does not contain the %s origin", label)
		}
	}
	return nil
}

func validCookieName(name string) bool {
	return cookieNamePattern.MatchString(name)
}

func validateRuntimePath(label string, value string) error {
	parsed, parseError := url.ParseRequestURI(value)
	if parseError != nil ||
		!strings.HasPrefix(value, "/") ||
		strings.HasPrefix(value, "//") ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		return fmt.Errorf("runtime %s must be one absolute path", label)
	}
	return nil
}
