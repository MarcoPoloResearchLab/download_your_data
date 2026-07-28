package library

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestRepositoryRejectsForeignStaleAndStructurallyInvalidState(
	testContext *testing.T,
) {
	testCases := []struct {
		name      string
		transform func([]byte) []byte
	}{
		{
			name: "foreign schema",
			transform: func(encoded []byte) []byte {
				return bytes.Replace(
					encoded,
					[]byte(`"schema_owner": "download_your_data"`),
					[]byte(`"schema_owner": "foreign_product"`),
					1,
				)
			},
		},
		{
			name: "stale schema",
			transform: func(encoded []byte) []byte {
				return bytes.Replace(
					encoded,
					[]byte(`"schema_version": "1"`),
					[]byte(`"schema_version": "0"`),
					1,
				)
			},
		},
		{
			name: "unknown field",
			transform: func(encoded []byte) []byte {
				return bytes.Replace(
					encoded,
					[]byte(`"revision": 1,`),
					[]byte(`"revision": 1, "legacy": true,`),
					1,
				)
			},
		},
		{
			name: "invalid active pointer",
			transform: func(encoded []byte) []byte {
				return bytes.Replace(
					encoded,
					[]byte(`"deleting": false,`),
					[]byte(`"deleting": false, "active_generation_id": "ng_11111111111111111111111111111111",`),
					1,
				)
			},
		},
	}
	for _, testCase := range testCases {
		testContext.Run(testCase.name, func(testContext *testing.T) {
			fixture := newWorkspaceFixture(testContext)
			workspace := fixture.open(testContext, workspaceOptions{
				now:     fixture.clock,
				entropy: testEntropy(0x61),
			})
			if closeError := workspace.Close(); closeError != nil {
				testContext.Fatalf("close repository fixture: %v", closeError)
			}
			encoded, readError := os.ReadFile(fixture.stateFile.Path())
			if readError != nil {
				testContext.Fatalf("read repository fixture: %v", readError)
			}
			corrupted := testCase.transform(encoded)
			if bytes.Equal(corrupted, encoded) {
				testContext.Fatalf("repository transform made no change")
			}
			if writeError := os.WriteFile(
				fixture.stateFile.Path(),
				corrupted,
				0o600,
			); writeError != nil {
				testContext.Fatalf("write invalid repository fixture: %v", writeError)
			}
			if chmodError := os.Chmod(fixture.stateFile.Path(), 0o600); chmodError != nil {
				testContext.Fatalf("set invalid repository fixture mode: %v", chmodError)
			}
			reopened, reopenError := openWorkspace(
				fixture.root,
				fixture.stateFile,
				fixture.leaseFile,
				fixture.cacheFile,
				false,
				workspaceOptions{
					now:     fixture.clock,
					entropy: testEntropy(0x62),
				},
			)
			if reopened != nil {
				reopened.Close()
			}
			if errorCode(reopenError) != ErrorInvalidPersistence {
				testContext.Fatalf("invalid repository error = %v", reopenError)
			}
		})
	}
}

func TestRepositoryAndLeaseRemainOwnerOnlyAndContainNoActivityData(
	testContext *testing.T,
) {
	fixture := newWorkspaceFixture(testContext)
	workspace := fixture.open(testContext, workspaceOptions{
		now:     fixture.clock,
		entropy: testEntropy(0x71),
	})
	defer workspace.Close()
	generation, createError := workspace.CreateLocalGeneration()
	if createError != nil {
		testContext.Fatalf("create repository privacy fixture: %v", createError)
	}
	if _, uploadError := workspace.UploadViewingActivity(
		testContext.Context(),
		generation.ID,
		strings.NewReader(syntheticViewingCSV),
	); uploadError != nil {
		testContext.Fatalf("upload repository privacy fixture: %v", uploadError)
	}
	waitForGenerationState(testContext, workspace, generation.ID, GenerationStateReady)

	for _, file := range []string{
		fixture.stateFile.Path(),
		fixture.leaseFile.Path(),
	} {
		pathInfo, statError := os.Stat(file)
		if statError != nil {
			testContext.Fatalf("inspect repository file: %v", statError)
		}
		if pathInfo.Mode().Perm() != 0o600 {
			testContext.Fatalf("%s mode = %04o; want 0600", file, pathInfo.Mode().Perm())
		}
	}
	stateBytes, readError := os.ReadFile(fixture.stateFile.Path())
	if readError != nil {
		testContext.Fatalf("read repository state: %v", readError)
	}
	for _, forbidden := range []string{
		"Synthetic Film",
		"Synthetic Series",
		"1/1/26",
	} {
		if strings.Contains(string(stateBytes), forbidden) {
			testContext.Fatalf("repository state exposed activity data %q", forbidden)
		}
	}
}

func TestRepositoryRejectsPermissiveStateFileInsteadOfRepairingIt(
	testContext *testing.T,
) {
	fixture := newWorkspaceFixture(testContext)
	workspace := fixture.open(testContext, workspaceOptions{
		now:     fixture.clock,
		entropy: testEntropy(0x81),
	})
	if closeError := workspace.Close(); closeError != nil {
		testContext.Fatalf("close mode fixture: %v", closeError)
	}
	if chmodError := os.Chmod(fixture.stateFile.Path(), 0o644); chmodError != nil {
		testContext.Fatalf("make permissive state fixture: %v", chmodError)
	}
	reopened, reopenError := openWorkspace(
		fixture.root,
		fixture.stateFile,
		fixture.leaseFile,
		fixture.cacheFile,
		false,
		workspaceOptions{
			now:     fixture.clock,
			entropy: testEntropy(0x82),
		},
	)
	if reopened != nil {
		reopened.Close()
	}
	if errorCode(reopenError) != ErrorPersistenceFailed {
		testContext.Fatalf("permissive state file should be rejected: %v", reopenError)
	}
	pathInfo, statError := os.Stat(fixture.stateFile.Path())
	if statError != nil {
		testContext.Fatalf("inspect rejected state file: %v", statError)
	}
	if pathInfo.Mode().Perm() != 0o644 {
		testContext.Fatalf(
			"rejected state mode = %04o; want unchanged 0644",
			pathInfo.Mode().Perm(),
		)
	}
}
