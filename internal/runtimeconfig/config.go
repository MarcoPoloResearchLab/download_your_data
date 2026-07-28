// Package runtimeconfig owns the validated process-wide local application configuration.
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

	"github.com/MarcoPoloResearchLab/download_your_data/internal/inference"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
)

const (
	AddressEnvironment           = "DOWNLOAD_YOUR_DATA_ADDRESS"
	DataDirectoryEnvironment     = "DOWNLOAD_YOUR_DATA_DATA_DIR"
	InferenceBoundaryEnvironment = "DOWNLOAD_YOUR_DATA_INFERENCE_BOUNDARY"
	DefaultListenAddress         = "127.0.0.1:8787"
	defaultDataDirectoryName     = ".download-your-data"
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
	ErrorCSRFEntropyUnavailable   ErrorCode = "csrf_entropy_unavailable"
	ErrorInvalidArchivePath       ErrorCode = "invalid_archive_path"
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
	archiveDatabase   privatepath.File
	inferenceBaseURL  inference.BaseURL
	inferenceBoundary InferenceBoundary
	csrfToken         string
}

// Load validates environment-backed configuration and creates the private data root.
func Load(
	lookupEnvironment func(string) string,
	userHomeDirectory string,
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
	homeDirectory, homeError := validateHomeDirectory(userHomeDirectory)
	if homeError != nil {
		return Config{}, newConfigurationError(ErrorInvalidDataRoot, homeError)
	}
	dataRootPath := strings.TrimSpace(lookupEnvironment(DataDirectoryEnvironment))
	if dataRootPath == "" {
		dataRootPath = filepath.Join(homeDirectory, defaultDataDirectoryName)
	}
	if filepath.Clean(dataRootPath) == homeDirectory {
		return Config{}, newConfigurationError(
			ErrorInvalidDataRoot,
			fmt.Errorf(
				"validate %s %q: the user home directory is too broad for application data",
				DataDirectoryEnvironment,
				dataRootPath,
			),
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
	archiveDatabase, databaseError := dataRoot.File(filepath.FromSlash(product.ArchiveDatabaseRelativePath))
	if databaseError != nil {
		return Config{}, newConfigurationError(
			ErrorInvalidArchivePath,
			fmt.Errorf("resolve archive database: %w", databaseError),
		)
	}

	return Config{
		listenAddress:     listenAddress,
		dataRoot:          dataRoot,
		archiveDatabase:   archiveDatabase,
		inferenceBaseURL:  inferenceBaseURL,
		inferenceBoundary: inferenceBoundary,
		csrfToken:         hex.EncodeToString(tokenHash[:]),
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

// ListenAddress returns the validated loopback server address.
func (config Config) ListenAddress() string {
	return config.listenAddress
}

// DataRoot returns the validated private filesystem root.
func (config Config) DataRoot() privatepath.Root {
	return config.dataRoot
}

// ArchiveDatabase returns the sole private conversation database location.
func (config Config) ArchiveDatabase() privatepath.File {
	return config.archiveDatabase
}

// InferenceBaseURL returns the validated inference server URL.
func (config Config) InferenceBaseURL() inference.BaseURL {
	return config.inferenceBaseURL
}

// InferenceBoundary returns the closed inference authorization state.
func (config Config) InferenceBoundary() InferenceBoundary {
	return config.inferenceBoundary
}

// CSRFToken returns the ephemeral process mutation token.
func (config Config) CSRFToken() string {
	return config.csrfToken
}

func newListenAddress(configuredAddress string) (string, error) {
	listenAddress := strings.TrimSpace(configuredAddress)
	if listenAddress == "" {
		listenAddress = DefaultListenAddress
	}
	host, portText, splitError := net.SplitHostPort(listenAddress)
	if splitError != nil {
		return "", fmt.Errorf("validate local application address %s: %w", listenAddress, splitError)
	}
	hostAddress := net.ParseIP(host)
	if hostAddress == nil || !hostAddress.IsLoopback() {
		return "", fmt.Errorf("validate local application address %s: host must be a loopback IP address", listenAddress)
	}
	port, portError := strconv.Atoi(portText)
	if portError != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("validate local application address %s: port must be between 1 and 65535", listenAddress)
	}
	return net.JoinHostPort(hostAddress.String(), strconv.Itoa(port)), nil
}

func validateHomeDirectory(userHomeDirectory string) (string, error) {
	homeDirectory := filepath.Clean(strings.TrimSpace(userHomeDirectory))
	if homeDirectory == "." || !filepath.IsAbs(homeDirectory) {
		return "", fmt.Errorf("validate user home directory %q: path must be absolute", userHomeDirectory)
	}
	if filepath.Dir(homeDirectory) == homeDirectory {
		return "", fmt.Errorf("validate user home directory %q: filesystem roots are not allowed", userHomeDirectory)
	}
	return homeDirectory, nil
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
