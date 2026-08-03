// Package uiconfig owns the public mpr-ui YAML document contract.
package uiconfig

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const maximumDocumentBytes = 4 * 1024

var tenantIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Input is the complete validated source for one browser-facing mpr-ui config.
type Input struct {
	Description       string
	PublicOrigin      string
	TAuthOrigin       string
	GoogleWebClientID string
	TenantID          string
	LoginPath         string
	LogoutPath        string
	NoncePath         string
	SessionPath       string
}

type document struct {
	Environments []environment `yaml:"environments"`
}

type environment struct {
	Description string   `yaml:"description"`
	Origins     []string `yaml:"origins"`
	Auth        auth     `yaml:"auth"`
}

type auth struct {
	TAuthURL       string `yaml:"tauthUrl"`
	GoogleClientID string `yaml:"googleClientId"`
	TenantID       string `yaml:"tenantId"`
	LoginPath      string `yaml:"loginPath"`
	LogoutPath     string `yaml:"logoutPath"`
	NoncePath      string `yaml:"noncePath"`
	SessionPath    string `yaml:"sessionPath"`
}

// Render validates and encodes one exact browser configuration document.
func Render(input Input) ([]byte, error) {
	if strings.TrimSpace(input.Description) == "" {
		return nil, errors.New("render browser configuration: description is required")
	}
	if originError := validateBrowserOrigin("public origin", input.PublicOrigin); originError != nil {
		return nil, originError
	}
	if originError := validateBrowserOrigin("TAuth origin", input.TAuthOrigin); originError != nil {
		return nil, originError
	}
	if !strings.HasSuffix(input.GoogleWebClientID, ".apps.googleusercontent.com") ||
		strings.TrimSpace(input.GoogleWebClientID) != input.GoogleWebClientID {
		return nil, errors.New("render browser configuration: Google web client ID is invalid")
	}
	if !tenantIDPattern.MatchString(input.TenantID) {
		return nil, errors.New("render browser configuration: tenant ID is invalid")
	}
	for label, configuredPath := range map[string]string{
		"login path":   input.LoginPath,
		"logout path":  input.LogoutPath,
		"nonce path":   input.NoncePath,
		"session path": input.SessionPath,
	} {
		if pathError := validateAbsolutePath(label, configuredPath); pathError != nil {
			return nil, pathError
		}
	}

	encoded, encodeError := yaml.Marshal(document{
		Environments: []environment{{
			Description: input.Description,
			Origins:     []string{input.PublicOrigin},
			Auth: auth{
				TAuthURL:       input.TAuthOrigin,
				GoogleClientID: input.GoogleWebClientID,
				TenantID:       input.TenantID,
				LoginPath:      input.LoginPath,
				LogoutPath:     input.LogoutPath,
				NoncePath:      input.NoncePath,
				SessionPath:    input.SessionPath,
			},
		}},
	})
	if encodeError != nil {
		return nil, fmt.Errorf("render browser configuration: %w", encodeError)
	}
	if len(encoded) > maximumDocumentBytes {
		return nil, errors.New("render browser configuration: document exceeds the public size limit")
	}
	return encoded, nil
}

func validateBrowserOrigin(label string, value string) error {
	parsed, parseError := url.Parse(value)
	if parseError != nil ||
		(parsed.Scheme != "https" && parsed.Scheme != "http") ||
		parsed.Host == "" ||
		parsed.User != nil ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") {
		return fmt.Errorf("render browser configuration: %s must be one HTTP origin", label)
	}
	if parsed.Scheme == "http" {
		hostAddress := net.ParseIP(parsed.Hostname())
		if !strings.EqualFold(parsed.Hostname(), "localhost") &&
			(hostAddress == nil || !hostAddress.IsLoopback()) {
			return fmt.Errorf("render browser configuration: %s requires HTTPS outside loopback", label)
		}
	}
	return nil
}

func validateAbsolutePath(label string, value string) error {
	parsed, parseError := url.ParseRequestURI(value)
	if parseError != nil ||
		!strings.HasPrefix(value, "/") ||
		strings.HasPrefix(value, "//") ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		return fmt.Errorf("render browser configuration: %s must be one absolute path", label)
	}
	return nil
}
