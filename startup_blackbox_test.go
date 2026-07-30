package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/inference"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/tmdb"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
)

func TestExecutableRejectsInvalidRuntimeConfigurationBeforeServing(testContext *testing.T) {
	binaryPath := filepath.Join(testContext.TempDir(), "download-your-data")
	buildCommand := exec.Command("go", "build", "-o", binaryPath, ".")
	buildOutput, buildError := buildCommand.CombinedOutput()
	if buildError != nil {
		testContext.Fatalf("build application executable: %v\n%s", buildError, buildOutput)
	}

	testCases := []struct {
		name         string
		environment  map[string]string
		expectedCode runtimeconfig.ErrorCode
	}{
		{
			name: "hostname listen address",
			environment: map[string]string{
				runtimeconfig.AddressEnvironment: "download-your-data:8787",
			},
			expectedCode: runtimeconfig.ErrorInvalidListenAddress,
		},
		{
			name: "relative data root",
			environment: map[string]string{
				runtimeconfig.DataDirectoryEnvironment: "application-data",
			},
			expectedCode: runtimeconfig.ErrorInvalidDataRoot,
		},
		{
			name: "invalid inference URL",
			environment: map[string]string{
				inference.BaseURLEnvironment: "ftp://127.0.0.1:1234/v1",
			},
			expectedCode: runtimeconfig.ErrorInvalidInferenceURL,
		},
		{
			name: "unauthorized remote inference",
			environment: map[string]string{
				inference.BaseURLEnvironment: "https://inference.example.com/v1",
			},
			expectedCode: runtimeconfig.ErrorInvalidInferenceBoundary,
		},
		{
			name: "invalid TMDB token",
			environment: map[string]string{
				tmdb.ReadTokenEnvironment: " private-token ",
			},
			expectedCode: runtimeconfig.ErrorInvalidTMDBToken,
		},
	}

	for _, testCase := range testCases {
		testContext.Run(testCase.name, func(testContext *testing.T) {
			dataDirectory := filepath.Join(testContext.TempDir(), "application-data")
			environment := testRuntimeEnvironment(dataDirectory)
			for key, value := range testCase.environment {
				environment[key] = value
			}

			command := exec.Command(binaryPath)
			command.Env = replacementEnvironment(os.Environ(), environment)
			output, runError := command.CombinedOutput()
			if runError == nil {
				testContext.Fatalf("invalid runtime configuration started successfully")
			}
			if !strings.Contains(string(output), "error_type="+string(testCase.expectedCode)) {
				testContext.Fatalf(
					"startup output = %q; want typed code %q",
					output,
					testCase.expectedCode,
				)
			}
			if _, statError := os.Stat(dataDirectory); !os.IsNotExist(statError) {
				testContext.Fatalf("invalid startup created data directory %q", dataDirectory)
			}
		})
	}
}

func replacementEnvironment(current []string, replacements map[string]string) []string {
	replacedKeys := map[string]struct{}{
		"HOME":                                      {},
		runtimeconfig.AddressEnvironment:            {},
		runtimeconfig.DataDirectoryEnvironment:      {},
		inference.BaseURLEnvironment:                {},
		runtimeconfig.InferenceBoundaryEnvironment:  {},
		tmdb.ReadTokenEnvironment:                   {},
		runtimeconfig.PublicOriginEnvironment:       {},
		runtimeconfig.APIOriginEnvironment:          {},
		runtimeconfig.TAuthURLEnvironment:           {},
		runtimeconfig.TAuthTenantIDEnvironment:      {},
		runtimeconfig.TAuthJWTSigningKeyEnvironment: {},
		runtimeconfig.TAuthSessionCookieEnvironment: {},
		runtimeconfig.TAuthRefreshCookieEnvironment: {},
		runtimeconfig.GoogleClientIDEnvironment:     {},
	}
	environment := make([]string, 0, len(current)+len(replacements))
	for _, entry := range current {
		key, _, found := strings.Cut(entry, "=")
		if _, replace := replacedKeys[key]; found && replace {
			continue
		}
		environment = append(environment, entry)
	}
	for key, value := range replacements {
		environment = append(environment, key+"="+value)
	}
	return environment
}
