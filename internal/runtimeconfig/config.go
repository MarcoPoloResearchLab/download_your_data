// Package runtimeconfig owns the validated process-wide application configuration.
package runtimeconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/authentication"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/inference"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
)

const (
	AddressEnvironment           = "DOWNLOAD_YOUR_DATA_ADDRESS"
	DataDirectoryEnvironment     = "DOWNLOAD_YOUR_DATA_DATA_DIR"
	InferenceBoundaryEnvironment = "DOWNLOAD_YOUR_DATA_INFERENCE_BOUNDARY"
	DefaultListenAddress         = "127.0.0.1:8787"
	csrfEntropyBytes             = 32
)

// ErrorCode is the non-sensitive startup failure identity used in process logs.
type ErrorCode string

const (
	ErrorEnvironmentLookupMissing ErrorCode = "environment_lookup_missing"
	ErrorEntropySourceMissing     ErrorCode = "entropy_source_missing"
	ErrorInvalidListenAddress     ErrorCode = "invalid_listen_address"
	ErrorInvalidDataRoot          ErrorCode = "invalid_data_root"
	ErrorInvalidInferenceURL      ErrorCode = "invalid_inference_url"
	ErrorInvalidInferenceBoundary ErrorCode = "invalid_inference_boundary"
	ErrorInvalidTMDBToken         ErrorCode = "invalid_tmdb_token"
	ErrorCSRFEntropyUnavailable   ErrorCode = "csrf_entropy_unavailable"
	ErrorInvalidAuthentication    ErrorCode = "invalid_authentication"
	ErrorUnknownRuntimeConfig     ErrorCode = "invalid_runtime_configuration"
)

// ConfigurationError preserves an actionable local cause and a non-sensitive log code.
type ConfigurationError struct {
	code  ErrorCode
	cause error
}

func (configurationError *ConfigurationError) Error() string {
	return configurationError.cause.Error()
}

func (configurationError *ConfigurationError) Unwrap() error {
	return configurationError.cause
}

// InferenceBoundary is the closed authorization state for the configured inference URL.
type InferenceBoundary string

const (
	// InferenceBoundaryLoopback permits only local loopback inference.
	InferenceBoundaryLoopback InferenceBoundary = "loopback"
	// InferenceBoundaryAuthorizedRemote explicitly permits the configured remote inference server.
	InferenceBoundaryAuthorizedRemote InferenceBoundary = "authorized-remote"
)

// Config is the complete validated process configuration.
type Config struct {
	listenAddress     string
	dataRoot          privatepath.Root
	inferenceBaseURL  inference.BaseURL
	inferenceBoundary InferenceBoundary
	tmdbReadToken     tmdb.ReadToken
	tmdbConfigured    bool
	csrfToken         string
	authentication    Authentication
}

// Load validates environment-backed configuration and creates the private data root.
func Load(
	lookupEnvironment func(string) string,
	entropy io.Reader,
) (Config, error) {
	if lookupEnvironment == nil {
		return Config{}, newConfigurationError(
			ErrorEnvironmentLookupMissing,
			fmt.Errorf("load runtime configuration: environment lookup is required"),
		)
	}
	if entropy == nil {
		return Config{}, newConfigurationError(
			ErrorEntropySourceMissing,
			fmt.Errorf("load runtime configuration: entropy source is required"),
		)
	}

	listenAddress, addressError := newListenAddress(lookupEnvironment(AddressEnvironment))
	if addressError != nil {
		return Config{}, newConfigurationError(ErrorInvalidListenAddress, addressError)
	}
	authenticationConfig, authenticationError := loadAuthentication(lookupEnvironment)
	if authenticationError != nil {
		return Config{}, newConfigurationError(
			ErrorInvalidAuthentication,
			authenticationError,
		)
	}
	dataRootPath := strings.TrimSpace(lookupEnvironment(DataDirectoryEnvironment))
	if dataRootPath == "" {
		return Config{}, newConfigurationError(
			ErrorInvalidDataRoot,
			fmt.Errorf("validate %s: value is required", DataDirectoryEnvironment),
		)
	}

	inferenceBaseURL, inferenceURLError := inference.NewBaseURL(
		lookupEnvironment(inference.BaseURLEnvironment),
	)
	if inferenceURLError != nil {
		return Config{}, newConfigurationError(ErrorInvalidInferenceURL, inferenceURLError)
	}
	inferenceBoundary, boundaryError := newInferenceBoundary(
		lookupEnvironment(InferenceBoundaryEnvironment),
		inferenceBaseURL,
	)
	if boundaryError != nil {
		return Config{}, newConfigurationError(ErrorInvalidInferenceBoundary, boundaryError)
	}
	tmdbReadToken, tmdbConfigured, tmdbTokenError := tmdb.OptionalReadToken(
		lookupEnvironment(tmdb.ReadTokenEnvironment),
	)
	if tmdbTokenError != nil {
		return Config{}, newConfigurationError(ErrorInvalidTMDBToken, tmdbTokenError)
	}

	csrfEntropy := make([]byte, csrfEntropyBytes)
	if _, entropyError := io.ReadFull(entropy, csrfEntropy); entropyError != nil {
		return Config{}, newConfigurationError(
			ErrorCSRFEntropyUnavailable,
			fmt.Errorf("generate process CSRF token: %w", entropyError),
		)
	}
	tokenHash := sha256.Sum256(csrfEntropy)

	dataRoot, dataRootError := privatepath.NewRoot(dataRootPath)
	if dataRootError != nil {
		return Config{}, newConfigurationError(
			ErrorInvalidDataRoot,
			fmt.Errorf("validate %s: %w", DataDirectoryEnvironment, dataRootError),
		)
	}
	return Config{
		listenAddress:     listenAddress,
		dataRoot:          dataRoot,
		inferenceBaseURL:  inferenceBaseURL,
		inferenceBoundary: inferenceBoundary,
		tmdbReadToken:     tmdbReadToken,
		tmdbConfigured:    tmdbConfigured,
		csrfToken:         hex.EncodeToString(tokenHash[:]),
		authentication:    authenticationConfig,
	}, nil
}

// Code returns the non-sensitive typed identity of a runtime configuration error.
func Code(configurationError error) ErrorCode {
	var typedError *ConfigurationError
	if errors.As(configurationError, &typedError) {
		return typedError.code
	}
	return ErrorUnknownRuntimeConfig
}

func newConfigurationError(code ErrorCode, cause error) error {
	return &ConfigurationError{code: code, cause: cause}
}

// ListenAddress returns the validated server bind address.
func (config Config) ListenAddress() string {
	return config.listenAddress
}

// DataRoot returns the validated private filesystem root.
func (config Config) DataRoot() privatepath.Root {
	return config.dataRoot
}

// InferenceBaseURL returns the validated inference server URL.
func (config Config) InferenceBaseURL() inference.BaseURL {
	return config.inferenceBaseURL
}

// InferenceBoundary returns the closed inference authorization state.
func (config Config) InferenceBoundary() InferenceBoundary {
	return config.inferenceBoundary
}

// TMDBReadToken returns the server-only credential when configured.
func (config Config) TMDBReadToken() (tmdb.ReadToken, bool) {
	return config.tmdbReadToken, config.tmdbConfigured
}

// TMDBConfigured reports only whether the server credential is available.
func (config Config) TMDBConfigured() bool {
	return config.tmdbConfigured
}

// CSRFToken returns the ephemeral process mutation token.
func (config Config) CSRFToken() string {
	return config.csrfToken
}

// Authentication returns the complete validated TAuth and browser boundary.
func (config Config) Authentication() Authentication {
	return config.authentication
}

// UserWorkspace resolves the sole private workspace for an authenticated user.
func (config Config) UserWorkspace(
	user authentication.AuthenticatedUser,
) (UserWorkspace, error) {
	if validationError := user.Validate(); validationError != nil {
		return UserWorkspace{}, fmt.Errorf("resolve user workspace: %w", validationError)
	}
	userDirectory, directoryError := config.dataRoot.EnsureDirectory(
		filepath.Join("users", user.StorageID()),
	)
	if directoryError != nil {
		return UserWorkspace{}, fmt.Errorf("resolve user workspace directory: %w", directoryError)
	}
	userRoot, rootError := privatepath.NewRoot(userDirectory.Path())
	if rootError != nil {
		return UserWorkspace{}, fmt.Errorf("open user workspace root: %w", rootError)
	}
	workspace, workspaceError := newUserWorkspace(userRoot)
	if workspaceError != nil {
		return UserWorkspace{}, fmt.Errorf("resolve user workspace paths: %w", workspaceError)
	}
	return workspace, nil
}

// DeleteUserWorkspace removes every application-owned artifact for one
// authenticated user.
func (config Config) DeleteUserWorkspace(
	user authentication.AuthenticatedUser,
) error {
	if validationError := user.Validate(); validationError != nil {
		return fmt.Errorf("delete user workspace: %w", validationError)
	}
	if removeError := config.dataRoot.RemoveDirectory(
		filepath.Join("users", user.StorageID()),
	); removeError != nil {
		return fmt.Errorf("delete user workspace: %w", removeError)
	}
	return nil
}

func newListenAddress(configuredAddress string) (string, error) {
	listenAddress := strings.TrimSpace(configuredAddress)
	if listenAddress == "" {
		listenAddress = DefaultListenAddress
	}
	host, portText, splitError := net.SplitHostPort(listenAddress)
	if splitError != nil {
		return "", fmt.Errorf("validate application address %s: %w", listenAddress, splitError)
	}
	hostAddress := net.ParseIP(host)
	if hostAddress == nil {
		return "", fmt.Errorf("validate application address %s: host must be an IP address", listenAddress)
	}
	port, portError := strconv.Atoi(portText)
	if portError != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("validate application address %s: port must be between 1 and 65535", listenAddress)
	}
	return net.JoinHostPort(hostAddress.String(), strconv.Itoa(port)), nil
}

func newInferenceBoundary(
	configuredBoundary string,
	baseURL inference.BaseURL,
) (InferenceBoundary, error) {
	boundaryValue := strings.TrimSpace(configuredBoundary)
	if boundaryValue == "" {
		boundaryValue = string(InferenceBoundaryLoopback)
	}
	boundary := InferenceBoundary(boundaryValue)
	switch boundary {
	case InferenceBoundaryLoopback:
		if !baseURL.IsLoopback() {
			return "", fmt.Errorf(
				"validate %s: inference URL %s is remote; set %s=%s to authorize it",
				InferenceBoundaryEnvironment,
				baseURL.String(),
				InferenceBoundaryEnvironment,
				InferenceBoundaryAuthorizedRemote,
			)
		}
	case InferenceBoundaryAuthorizedRemote:
		if baseURL.IsLoopback() {
			return "", fmt.Errorf(
				"validate %s: %s requires a non-loopback inference URL",
				InferenceBoundaryEnvironment,
				InferenceBoundaryAuthorizedRemote,
			)
		}
	default:
		return "", fmt.Errorf(
			"validate %s %q: use %s or %s",
			InferenceBoundaryEnvironment,
			configuredBoundary,
			InferenceBoundaryLoopback,
			InferenceBoundaryAuthorizedRemote,
		)
	}
	return boundary, nil
}
