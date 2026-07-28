package runtimeconfig_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/inference"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/product"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
)

func TestLoadBuildsTheCanonicalLocalRuntime(testContext *testing.T) {
	homeDirectory := testContext.TempDir()
	config, configError := runtimeconfig.Load(
		func(string) string { return "" },
		homeDirectory,
		bytes.NewReader(make([]byte, 32)),
	)
	if configError != nil {
		testContext.Fatalf("load default runtime config: %v", configError)
	}
	expectedRoot := filepath.Join(homeDirectory, ".download-your-data")
	expectedRoot, resolveError := filepath.EvalSymlinks(expectedRoot)
	if resolveError != nil {
		testContext.Fatalf("resolve expected data root: %v", resolveError)
	}
	if config.ListenAddress() != runtimeconfig.DefaultListenAddress {
		testContext.Fatalf("unexpected listen address %q", config.ListenAddress())
	}
	if config.DataRoot().Path() != expectedRoot {
		testContext.Fatalf("data root = %q; want %q", config.DataRoot().Path(), expectedRoot)
	}
	if config.ArchiveDatabase().RelativePath() != filepath.FromSlash(product.ArchiveDatabaseRelativePath) {
		testContext.Fatalf("unexpected archive path %q", config.ArchiveDatabase().RelativePath())
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
}

func TestLoadAcceptsOnlyExplicitlyAuthorizedRemoteInference(testContext *testing.T) {
	dataDirectory := privateDataDirectory(testContext)
	environment := map[string]string{
		runtimeconfig.DataDirectoryEnvironment:     dataDirectory,
		inference.BaseURLEnvironment:               "https://inference.example.com/v1/",
		runtimeconfig.InferenceBoundaryEnvironment: string(runtimeconfig.InferenceBoundaryAuthorizedRemote),
	}
	config, configError := runtimeconfig.Load(
		func(key string) string { return environment[key] },
		testContext.TempDir(),
		bytes.NewReader(make([]byte, 32)),
	)
	if configError != nil {
		testContext.Fatalf("load authorized remote config: %v", configError)
	}
	if config.InferenceBaseURL().String() != "https://inference.example.com/v1" ||
		config.InferenceBoundary() != runtimeconfig.InferenceBoundaryAuthorizedRemote {
		testContext.Fatalf("unexpected remote inference config")
	}
}

func TestLoadRejectsInvalidStartupConfiguration(testContext *testing.T) {
	testCases := []struct {
		name         string
		environment  map[string]string
		home         string
		expectedCode runtimeconfig.ErrorCode
		expectedText string
	}{
		{
			name: "public listen address",
			environment: map[string]string{
				runtimeconfig.AddressEnvironment:       "0.0.0.0:8787",
				runtimeconfig.DataDirectoryEnvironment: privateDataDirectory(testContext),
			},
			home:         testContext.TempDir(),
			expectedCode: runtimeconfig.ErrorInvalidListenAddress,
			expectedText: "host must be a loopback IP address",
		},
		{
			name: "unsafe home data root",
			environment: func() map[string]string {
				homeDirectory := testContext.TempDir()
				return map[string]string{
					runtimeconfig.DataDirectoryEnvironment: homeDirectory,
					"test_home":                            homeDirectory,
				}
			}(),
			expectedCode: runtimeconfig.ErrorInvalidDataRoot,
			expectedText: "user home directory is too broad",
		},
		{
			name: "relative data root",
			environment: map[string]string{
				runtimeconfig.DataDirectoryEnvironment: "relative/data",
			},
			home:         testContext.TempDir(),
			expectedCode: runtimeconfig.ErrorInvalidDataRoot,
			expectedText: "path must be absolute",
		},
		{
			name: "remote without authorization",
			environment: map[string]string{
				runtimeconfig.DataDirectoryEnvironment: privateDataDirectory(testContext),
				inference.BaseURLEnvironment:           "https://inference.example.com/v1",
			},
			home:         testContext.TempDir(),
			expectedCode: runtimeconfig.ErrorInvalidInferenceBoundary,
			expectedText: "set DOWNLOAD_YOUR_DATA_INFERENCE_BOUNDARY=authorized-remote",
		},
		{
			name: "remote authorization for loopback",
			environment: map[string]string{
				runtimeconfig.DataDirectoryEnvironment:     privateDataDirectory(testContext),
				runtimeconfig.InferenceBoundaryEnvironment: string(runtimeconfig.InferenceBoundaryAuthorizedRemote),
			},
			home:         testContext.TempDir(),
			expectedCode: runtimeconfig.ErrorInvalidInferenceBoundary,
			expectedText: "requires a non-loopback inference URL",
		},
		{
			name: "unknown inference boundary",
			environment: map[string]string{
				runtimeconfig.DataDirectoryEnvironment:     privateDataDirectory(testContext),
				runtimeconfig.InferenceBoundaryEnvironment: "sometimes-remote",
			},
			home:         testContext.TempDir(),
			expectedCode: runtimeconfig.ErrorInvalidInferenceBoundary,
			expectedText: "use loopback or authorized-remote",
		},
		{
			name: "inference credentials",
			environment: map[string]string{
				runtimeconfig.DataDirectoryEnvironment: privateDataDirectory(testContext),
				inference.BaseURLEnvironment:           "http://user:secret@localhost:1234/v1",
			},
			home:         testContext.TempDir(),
			expectedCode: runtimeconfig.ErrorInvalidInferenceURL,
			expectedText: "credentials are not allowed",
		},
	}
	for _, testCase := range testCases {
		testContext.Run(testCase.name, func(testContext *testing.T) {
			homeDirectory := testCase.home
			if homeDirectory == "" {
				homeDirectory = testCase.environment["test_home"]
			}
			_, configError := runtimeconfig.Load(
				func(key string) string { return testCase.environment[key] },
				homeDirectory,
				bytes.NewReader(make([]byte, 32)),
			)
			if configError == nil || !strings.Contains(configError.Error(), testCase.expectedText) {
				testContext.Fatalf("runtime config error = %v; want text %q", configError, testCase.expectedText)
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

func TestLoadRejectsAnUnreadableEntropySource(testContext *testing.T) {
	dataDirectory := privateDataDirectory(testContext)
	environment := map[string]string{
		runtimeconfig.DataDirectoryEnvironment: dataDirectory,
	}
	_, configError := runtimeconfig.Load(
		func(key string) string { return environment[key] },
		testContext.TempDir(),
		errorReader{},
	)
	if configError == nil || !strings.Contains(configError.Error(), "generate process CSRF token") {
		testContext.Fatalf("unexpected entropy error: %v", configError)
	}
	if runtimeconfig.Code(configError) != runtimeconfig.ErrorCSRFEntropyUnavailable {
		testContext.Fatalf("unexpected entropy error code %q", runtimeconfig.Code(configError))
	}
	if _, statError := os.Stat(dataDirectory); !os.IsNotExist(statError) {
		testContext.Fatalf("invalid runtime configuration created data root %q", dataDirectory)
	}
}

func TestLoadRejectsRemoteInferenceBeforeCreatingTheDataRoot(testContext *testing.T) {
	dataDirectory := privateDataDirectory(testContext)
	environment := map[string]string{
		runtimeconfig.DataDirectoryEnvironment: dataDirectory,
		inference.BaseURLEnvironment:           "https://inference.example.com/v1",
	}
	_, configError := runtimeconfig.Load(
		func(key string) string { return environment[key] },
		testContext.TempDir(),
		bytes.NewReader(make([]byte, 32)),
	)
	if configError == nil || !strings.Contains(configError.Error(), "authorized-remote") {
		testContext.Fatalf("unexpected remote inference error: %v", configError)
	}
	if _, statError := os.Stat(dataDirectory); !os.IsNotExist(statError) {
		testContext.Fatalf("invalid runtime configuration created data root %q", dataDirectory)
	}
}

type errorReader struct{}

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
