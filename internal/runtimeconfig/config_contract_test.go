package runtimeconfig_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/authentication"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/inference"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
)

func TestLoadBuildsTheCanonicalAuthenticatedRuntime(testContext *testing.T) {
	dataDirectory := privateDataDirectory(testContext)
	environment := validRuntimeEnvironment(dataDirectory)
	config, configError := runtimeconfig.Load(
		func(key string) string { return environment[key] },
		bytes.NewReader(make([]byte, 32)),
	)
	if configError != nil {
		testContext.Fatalf("load runtime config: %v", configError)
	}
	expectedRoot, resolveError := filepath.EvalSymlinks(dataDirectory)
	if resolveError != nil {
		testContext.Fatalf("resolve expected data root: %v", resolveError)
	}
	if config.ListenAddress() != runtimeconfig.DefaultListenAddress {
		testContext.Fatalf("unexpected listen address %q", config.ListenAddress())
	}
	if config.DataRoot().Path() != expectedRoot {
		testContext.Fatalf("data root = %q; want %q", config.DataRoot().Path(), expectedRoot)
	}
	authConfig := config.Authentication()
	if authConfig.PublicOrigin() != environment[runtimeconfig.PublicOriginEnvironment] ||
		authConfig.APIOrigin() != environment[runtimeconfig.APIOriginEnvironment] ||
		authConfig.TAuthURL() != environment[runtimeconfig.TAuthURLEnvironment] ||
		authConfig.TenantID() != environment[runtimeconfig.TAuthTenantIDEnvironment] ||
		authConfig.SessionCookieName() != environment[runtimeconfig.TAuthSessionCookieEnvironment] ||
		authConfig.RefreshCookieName() != environment[runtimeconfig.TAuthRefreshCookieEnvironment] ||
		authConfig.GoogleClientID() != environment[runtimeconfig.GoogleClientIDEnvironment] {
		testContext.Fatalf("unexpected authentication configuration")
	}
	validatorConfig := authConfig.SessionValidatorConfig()
	if validatorConfig.Issuer != runtimeconfig.TAuthJWTIssuer ||
		validatorConfig.CookieName != authConfig.SessionCookieName() ||
		string(validatorConfig.SigningKey) != environment[runtimeconfig.TAuthJWTSigningKeyEnvironment] {
		testContext.Fatalf("unexpected server-side session validator configuration")
	}

	user, userError := authentication.NewAuthenticatedUser(
		authConfig.TenantID(),
		"user-123",
	)
	if userError != nil {
		testContext.Fatalf("create authenticated user: %v", userError)
	}
	workspace, workspaceError := config.UserWorkspace(user)
	if workspaceError != nil {
		testContext.Fatalf("resolve authenticated workspace: %v", workspaceError)
	}
	expectedUserRoot := filepath.Join(expectedRoot, "users", user.StorageID())
	if workspace.Root().Path() != expectedUserRoot {
		testContext.Fatalf(
			"user root = %q; want %q",
			workspace.Root().Path(),
			expectedUserRoot,
		)
	}
	if workspace.ArchiveDatabase().RelativePath() != filepath.FromSlash(
		product.ArchiveDatabaseRelativePath,
	) {
		testContext.Fatalf(
			"unexpected user archive path %q",
			workspace.ArchiveDatabase().RelativePath(),
		)
	}
	if workspace.NetflixTMDBCache().RelativePath() != filepath.FromSlash(
		product.NetflixTMDBCacheRelativePath,
	) {
		testContext.Fatalf(
			"unexpected user Netflix cache path %q",
			workspace.NetflixTMDBCache().RelativePath(),
		)
	}
	if workspace.NetflixLibrary().RelativePath() != filepath.FromSlash(
		product.NetflixLibraryStateRelativePath,
	) {
		testContext.Fatalf(
			"unexpected user Netflix library path %q",
			workspace.NetflixLibrary().RelativePath(),
		)
	}
	if workspace.NetflixLease().RelativePath() != filepath.FromSlash(
		product.NetflixLibraryLeaseRelativePath,
	) {
		testContext.Fatalf(
			"unexpected user Netflix lease path %q",
			workspace.NetflixLease().RelativePath(),
		)
	}
	if config.TMDBConfigured() {
		testContext.Fatalf("TMDB must be not configured by default")
	}
	if _, configured := config.TMDBReadToken(); configured {
		testContext.Fatalf("default runtime exposed a TMDB credential")
	}
	if config.InferenceBaseURL().String() != inference.DefaultBaseURL ||
		config.InferenceBoundary() != runtimeconfig.InferenceBoundaryLoopback {
		testContext.Fatalf(
			"unexpected inference contract: url=%q boundary=%q",
			config.InferenceBaseURL().String(),
			config.InferenceBoundary(),
		)
	}
	if len(config.CSRFToken()) != 64 {
		testContext.Fatalf("CSRF token length = %d; want 64", len(config.CSRFToken()))
	}
	assertMode(testContext, config.DataRoot().Path(), 0o700)
	assertMode(testContext, workspace.Root().Path(), 0o700)
}

func TestLoadKeepsTheTMDBReadTokenServerOnly(testContext *testing.T) {
	environment := validRuntimeEnvironment(privateDataDirectory(testContext))
	environment[tmdb.ReadTokenEnvironment] = "private-test-read-token"
	config, configError := runtimeconfig.Load(
		func(key string) string { return environment[key] },
		bytes.NewReader(make([]byte, 32)),
	)
	if configError != nil {
		testContext.Fatalf("load TMDB runtime config: %v", configError)
	}
	readToken, configured := config.TMDBReadToken()
	if !configured || !config.TMDBConfigured() {
		testContext.Fatalf("configured token was not represented as available")
	}
	if _, clientError := tmdb.NewClient(readToken); clientError != nil {
		testContext.Fatalf("construct server-owned TMDB client: %v", clientError)
	}
}

func TestLoadAcceptsHostedContainerAndAuthorizedRemoteInference(testContext *testing.T) {
	environment := validRuntimeEnvironment(privateDataDirectory(testContext))
	environment[runtimeconfig.AddressEnvironment] = "0.0.0.0:8080"
	environment[runtimeconfig.PublicOriginEnvironment] = "https://dyd.example"
	environment[runtimeconfig.APIOriginEnvironment] = "https://dyd-api.example"
	environment[runtimeconfig.TAuthURLEnvironment] = "https://dyd-api.example"
	environment[inference.BaseURLEnvironment] = "https://inference.example.com/v1/"
	environment[runtimeconfig.InferenceBoundaryEnvironment] = string(
		runtimeconfig.InferenceBoundaryAuthorizedRemote,
	)
	config, configError := runtimeconfig.Load(
		func(key string) string { return environment[key] },
		bytes.NewReader(make([]byte, 32)),
	)
	if configError != nil {
		testContext.Fatalf("load hosted runtime config: %v", configError)
	}
	if config.ListenAddress() != "0.0.0.0:8080" ||
		config.InferenceBaseURL().String() != "https://inference.example.com/v1" ||
		config.InferenceBoundary() != runtimeconfig.InferenceBoundaryAuthorizedRemote {
		testContext.Fatalf("unexpected hosted runtime config")
	}
}

func TestLoadRejectsInvalidStartupConfiguration(testContext *testing.T) {
	testCases := []struct {
		name         string
		mutate       func(map[string]string)
		expectedCode runtimeconfig.ErrorCode
		expectedText string
	}{
		{
			name: "hostname listen address",
			mutate: func(environment map[string]string) {
				environment[runtimeconfig.AddressEnvironment] = "download-your-data:8787"
			},
			expectedCode: runtimeconfig.ErrorInvalidListenAddress,
			expectedText: "host must be an IP address",
		},
		{
			name: "missing data root",
			mutate: func(environment map[string]string) {
				delete(environment, runtimeconfig.DataDirectoryEnvironment)
			},
			expectedCode: runtimeconfig.ErrorInvalidDataRoot,
			expectedText: "value is required",
		},
		{
			name: "relative data root",
			mutate: func(environment map[string]string) {
				environment[runtimeconfig.DataDirectoryEnvironment] = "relative/data"
			},
			expectedCode: runtimeconfig.ErrorInvalidDataRoot,
			expectedText: "path must be absolute",
		},
		{
			name: "missing public origin",
			mutate: func(environment map[string]string) {
				delete(environment, runtimeconfig.PublicOriginEnvironment)
			},
			expectedCode: runtimeconfig.ErrorInvalidAuthentication,
			expectedText: runtimeconfig.PublicOriginEnvironment,
		},
		{
			name: "insecure hosted API origin",
			mutate: func(environment map[string]string) {
				environment[runtimeconfig.APIOriginEnvironment] = "http://dyd-api.example"
			},
			expectedCode: runtimeconfig.ErrorInvalidAuthentication,
			expectedText: "hosted origins require HTTPS",
		},
		{
			name: "short signing key",
			mutate: func(environment map[string]string) {
				environment[runtimeconfig.TAuthJWTSigningKeyEnvironment] = "short"
			},
			expectedCode: runtimeconfig.ErrorInvalidAuthentication,
			expectedText: "at least 32 bytes",
		},
		{
			name: "shared cookie names",
			mutate: func(environment map[string]string) {
				environment[runtimeconfig.TAuthRefreshCookieEnvironment] =
					environment[runtimeconfig.TAuthSessionCookieEnvironment]
			},
			expectedCode: runtimeconfig.ErrorInvalidAuthentication,
			expectedText: "must differ",
		},
		{
			name: "invalid cookie name",
			mutate: func(environment map[string]string) {
				environment[runtimeconfig.TAuthSessionCookieEnvironment] = "invalid cookie"
			},
			expectedCode: runtimeconfig.ErrorInvalidAuthentication,
			expectedText: "HTTP cookie token",
		},
		{
			name: "remote inference without authorization",
			mutate: func(environment map[string]string) {
				environment[inference.BaseURLEnvironment] = "https://inference.example.com/v1"
			},
			expectedCode: runtimeconfig.ErrorInvalidInferenceBoundary,
			expectedText: "set DOWNLOAD_YOUR_DATA_INFERENCE_BOUNDARY=authorized-remote",
		},
		{
			name: "remote authorization for loopback inference",
			mutate: func(environment map[string]string) {
				environment[runtimeconfig.InferenceBoundaryEnvironment] = string(
					runtimeconfig.InferenceBoundaryAuthorizedRemote,
				)
			},
			expectedCode: runtimeconfig.ErrorInvalidInferenceBoundary,
			expectedText: "requires a non-loopback inference URL",
		},
		{
			name: "unknown inference boundary",
			mutate: func(environment map[string]string) {
				environment[runtimeconfig.InferenceBoundaryEnvironment] = "sometimes-remote"
			},
			expectedCode: runtimeconfig.ErrorInvalidInferenceBoundary,
			expectedText: "use loopback or authorized-remote",
		},
		{
			name: "inference credentials",
			mutate: func(environment map[string]string) {
				environment[inference.BaseURLEnvironment] =
					"http://user:secret@localhost:1234/v1"
			},
			expectedCode: runtimeconfig.ErrorInvalidInferenceURL,
			expectedText: "credentials are not allowed",
		},
		{
			name: "invalid TMDB token",
			mutate: func(environment map[string]string) {
				environment[tmdb.ReadTokenEnvironment] = " private-token "
			},
			expectedCode: runtimeconfig.ErrorInvalidTMDBToken,
			expectedText: "trimmed UTF-8",
		},
	}
	for _, testCase := range testCases {
		testContext.Run(testCase.name, func(testContext *testing.T) {
			environment := validRuntimeEnvironment(privateDataDirectory(testContext))
			testCase.mutate(environment)
			_, configError := runtimeconfig.Load(
				func(key string) string { return environment[key] },
				bytes.NewReader(make([]byte, 32)),
			)
			if configError == nil || !strings.Contains(configError.Error(), testCase.expectedText) {
				testContext.Fatalf(
					"runtime config error = %v; want text %q",
					configError,
					testCase.expectedText,
				)
			}
			if runtimeconfig.Code(configError) != testCase.expectedCode {
				testContext.Fatalf(
					"runtime config error code = %q; want %q",
					runtimeconfig.Code(configError),
					testCase.expectedCode,
				)
			}
		})
	}
}

func TestLoadRejectsInvalidInputsBeforeCreatingTheDataRoot(testContext *testing.T) {
	testCases := []struct {
		name    string
		mutate  func(map[string]string)
		entropy errorReader
		code    runtimeconfig.ErrorCode
	}{
		{
			name: "authentication",
			mutate: func(environment map[string]string) {
				delete(environment, runtimeconfig.TAuthTenantIDEnvironment)
			},
			code: runtimeconfig.ErrorInvalidAuthentication,
		},
		{
			name: "remote inference",
			mutate: func(environment map[string]string) {
				environment[inference.BaseURLEnvironment] = "https://inference.example.com/v1"
			},
			code: runtimeconfig.ErrorInvalidInferenceBoundary,
		},
		{
			name:    "entropy",
			mutate:  func(map[string]string) {},
			entropy: errorReader{enabled: true},
			code:    runtimeconfig.ErrorCSRFEntropyUnavailable,
		},
	}
	for _, testCase := range testCases {
		testContext.Run(testCase.name, func(testContext *testing.T) {
			dataDirectory := privateDataDirectory(testContext)
			environment := validRuntimeEnvironment(dataDirectory)
			testCase.mutate(environment)
			var entropyReader interface {
				Read([]byte) (int, error)
			} = bytes.NewReader(make([]byte, 32))
			if testCase.entropy.enabled {
				entropyReader = testCase.entropy
			}
			_, configError := runtimeconfig.Load(
				func(key string) string { return environment[key] },
				entropyReader,
			)
			if runtimeconfig.Code(configError) != testCase.code {
				testContext.Fatalf("unexpected config error: %v", configError)
			}
			if _, statError := os.Stat(dataDirectory); !errors.Is(statError, os.ErrNotExist) {
				testContext.Fatalf("invalid runtime configuration created data root %q", dataDirectory)
			}
		})
	}
}

type errorReader struct {
	enabled bool
}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}

func assertMode(testContext *testing.T, path string, expected os.FileMode) {
	testContext.Helper()
	pathInfo, statError := os.Stat(path)
	if statError != nil {
		testContext.Fatalf("inspect %s: %v", path, statError)
	}
	if pathInfo.Mode().Perm() != expected {
		testContext.Fatalf("%s mode = %04o; want %04o", path, pathInfo.Mode().Perm(), expected)
	}
}

func privateDataDirectory(testContext *testing.T) string {
	testContext.Helper()
	return filepath.Join(testContext.TempDir(), "data")
}

func validRuntimeEnvironment(dataDirectory string) map[string]string {
	return map[string]string{
		runtimeconfig.DataDirectoryEnvironment: dataDirectory,
		runtimeconfig.PublicOriginEnvironment:  "http://127.0.0.1:4173",
		runtimeconfig.APIOriginEnvironment:     "http://127.0.0.1:8787",
		runtimeconfig.TAuthURLEnvironment:      "http://127.0.0.1:8787",
		runtimeconfig.TAuthTenantIDEnvironment: "download-your-data-test",
		runtimeconfig.TAuthJWTSigningKeyEnvironment: strings.Repeat(
			"test-signing-key-",
			2,
		),
		runtimeconfig.TAuthSessionCookieEnvironment: "app_session_dyd_test",
		runtimeconfig.TAuthRefreshCookieEnvironment: "app_refresh_dyd_test",
		runtimeconfig.GoogleClientIDEnvironment:     "test.apps.googleusercontent.com",
	}
}
