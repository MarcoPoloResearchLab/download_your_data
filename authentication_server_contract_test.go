package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/ingest"
	netflixlibrary "github.com/MarcoPoloResearchLab/download_your_data/internal/providers/netflix/library"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/store"
)

const (
	authorizationTestUserA = "authorization-user-a"
	authorizationTestUserB = "authorization-user-b"
)

func TestProtectedRoutesRequireOneValidTAuthSession(testContext *testing.T) {
	config := testRuntimeConfig(testContext)
	handler, handlerError := newApplicationHandler(
		config,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if handlerError != nil {
		testContext.Fatalf("create application handler: %v", handlerError)
	}
	defer handler.Close()
	server := httptest.NewServer(handler)
	defer server.Close()

	for _, publicPath := range []string{healthPath, uiConfigPath, "/#guide/netflix"} {
		response, requestError := http.Get(server.URL + publicPath)
		if requestError != nil {
			testContext.Fatalf("request public route %s: %v", publicPath, requestError)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			testContext.Fatalf(
				"public route %s status = %d; want %d",
				publicPath,
				response.StatusCode,
				http.StatusOK,
			)
		}
	}

	for _, protectedPath := range []string{
		capabilitiesPath,
		netflixProviderPath,
		openAIProviderPath,
	} {
		response, requestError := http.Get(server.URL + protectedPath)
		if requestError != nil {
			testContext.Fatalf("request protected route %s: %v", protectedPath, requestError)
		}
		assertRequestError(
			testContext,
			response,
			http.StatusUnauthorized,
			"session_required",
		)
	}

	invalidRequest, requestError := http.NewRequest(
		http.MethodGet,
		server.URL+capabilitiesPath,
		nil,
	)
	if requestError != nil {
		testContext.Fatalf("create invalid-session request: %v", requestError)
	}
	invalidRequest.AddCookie(&http.Cookie{
		Name:  config.Authentication().SessionCookieName(),
		Value: "not-a-jwt",
	})
	invalidResponse, requestError := http.DefaultClient.Do(invalidRequest)
	if requestError != nil {
		testContext.Fatalf("perform invalid-session request: %v", requestError)
	}
	assertRequestError(
		testContext,
		invalidResponse,
		http.StatusUnauthorized,
		"session_required",
	)

	authenticatedResponse := authenticatedUserRequest(
		testContext,
		config,
		server.URL,
		authorizationTestUserA,
		http.MethodGet,
		capabilitiesPath,
		"",
	)
	authenticatedResponse.Body.Close()
	if authenticatedResponse.StatusCode != http.StatusOK {
		testContext.Fatalf(
			"valid TAuth session status = %d; want %d",
			authenticatedResponse.StatusCode,
			http.StatusOK,
		)
	}
}

func TestAuthenticatedUsersHaveIsolatedProviderWorkspaces(
	testContext *testing.T,
) {
	config := testRuntimeConfig(testContext)
	handler, handlerError := newApplicationHandler(
		config,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if handlerError != nil {
		testContext.Fatalf("create application handler: %v", handlerError)
	}
	defer handler.Close()
	server := httptest.NewServer(handler)
	defer server.Close()

	userAGenerationID := importNetflixForUser(
		testContext,
		config,
		server.URL,
		authorizationTestUserA,
	)
	userBSnapshot := requestNetflixSnapshotForUser(
		testContext,
		config,
		server.URL,
		authorizationTestUserB,
	)
	if userBSnapshot.State != netflixlibrary.ProviderStateEmpty ||
		userBSnapshot.Active != nil ||
		userBSnapshot.Building != nil {
		testContext.Fatalf("user B observed user A Netflix state: %+v", userBSnapshot)
	}
	for _, crossUserPath := range []string{
		netflixGenerationsPath + "/" + userAGenerationID + "/events?after=0",
		netflixGenerationsPath + "/" + userAGenerationID + "/analytics",
		netflixGenerationsPath + "/" + userAGenerationID + "/records?limit=10",
		netflixGenerationsPath + "/" + userAGenerationID + "/export",
	} {
		crossUserResponse := authenticatedUserRequest(
			testContext,
			config,
			server.URL,
			authorizationTestUserB,
			http.MethodGet,
			crossUserPath,
			"",
		)
		assertRequestError(
			testContext,
			crossUserResponse,
			http.StatusNotFound,
			string(netflixlibrary.ErrorNotFound),
		)
	}
	crossUserUploadRequest, requestError := http.NewRequest(
		http.MethodPut,
		server.URL+netflixGenerationsPath+"/"+userAGenerationID+"/viewing-activity",
		strings.NewReader(httpSyntheticViewingCSV),
	)
	if requestError != nil {
		testContext.Fatalf("create cross-user Netflix upload request: %v", requestError)
	}
	crossUserUploadRequest.AddCookie(
		testSessionCookie(testContext, config, authorizationTestUserB),
	)
	crossUserUploadRequest.Header.Set("Origin", config.Authentication().PublicOrigin())
	crossUserUploadRequest.Header.Set(csrfHeaderName, config.CSRFToken())
	crossUserUploadRequest.Header.Set("Content-Type", "text/csv")
	crossUserUploadResponse, uploadError := http.DefaultClient.Do(crossUserUploadRequest)
	if uploadError != nil {
		testContext.Fatalf("perform cross-user Netflix upload: %v", uploadError)
	}
	assertRequestError(
		testContext,
		crossUserUploadResponse,
		http.StatusNotFound,
		string(netflixlibrary.ErrorNotFound),
	)
	crossUserDeleteResponse := authenticatedUserRequest(
		testContext,
		config,
		server.URL,
		authorizationTestUserB,
		http.MethodDelete,
		netflixGenerationsPath+"/"+userAGenerationID,
		"",
	)
	assertRequestError(
		testContext,
		crossUserDeleteResponse,
		http.StatusNotFound,
		string(netflixlibrary.ErrorNotFound),
	)

	userBGenerationID := importNetflixForUser(
		testContext,
		config,
		server.URL,
		authorizationTestUserB,
	)
	if userBGenerationID == userAGenerationID {
		testContext.Fatalf("independent user imports reused a generation identifier")
	}
	userASnapshot := requestNetflixSnapshotForUser(
		testContext,
		config,
		server.URL,
		authorizationTestUserA,
	)
	if userASnapshot.Active == nil || userASnapshot.Active.ID != userAGenerationID {
		testContext.Fatalf("user A Netflix state changed after user B import: %+v", userASnapshot)
	}

	seedOpenAIArchiveForUser(
		testContext,
		config,
		authorizationTestUserA,
	)
	userAOpenAI := requestOpenAISnapshotForUser(
		testContext,
		config,
		server.URL,
		authorizationTestUserA,
	)
	userBOpenAI := requestOpenAISnapshotForUser(
		testContext,
		config,
		server.URL,
		authorizationTestUserB,
	)
	if userAOpenAI.State != openAIStateIndexNeeded ||
		userAOpenAI.Statistics.Imports != 1 {
		testContext.Fatalf("user A OpenAI archive was not visible: %+v", userAOpenAI)
	}
	if userBOpenAI.State != openAIStateEmpty ||
		userBOpenAI.Statistics != (openAIArchiveStatistics{}) {
		testContext.Fatalf("user B observed user A OpenAI archive: %+v", userBOpenAI)
	}

	userAWorkspacePath := testUserWorkspace(
		testContext,
		config,
		authorizationTestUserA,
	).Root().Path()
	deleteResponse := authenticatedUserRequest(
		testContext,
		config,
		server.URL,
		authorizationTestUserA,
		http.MethodDelete,
		userWorkspacePath,
		`{"confirmation":"`+userWorkspaceDeleteConfirmation+`"}`,
	)
	deleteResponse.Body.Close()
	if deleteResponse.StatusCode != http.StatusNoContent {
		testContext.Fatalf(
			"delete user A workspace status = %d; want %d",
			deleteResponse.StatusCode,
			http.StatusNoContent,
		)
	}
	if _, statError := os.Stat(userAWorkspacePath); !errors.Is(statError, os.ErrNotExist) {
		testContext.Fatalf("deleted user A workspace remains: %v", statError)
	}

	remainingUserBSnapshot := requestNetflixSnapshotForUser(
		testContext,
		config,
		server.URL,
		authorizationTestUserB,
	)
	if remainingUserBSnapshot.Active == nil ||
		remainingUserBSnapshot.Active.ID != userBGenerationID {
		testContext.Fatalf(
			"user A deletion changed user B Netflix state: %+v",
			remainingUserBSnapshot,
		)
	}
	deletedUserAOpenAI := requestOpenAISnapshotForUser(
		testContext,
		config,
		server.URL,
		authorizationTestUserA,
	)
	if deletedUserAOpenAI.State != openAIStateEmpty ||
		deletedUserAOpenAI.Statistics != (openAIArchiveStatistics{}) {
		testContext.Fatalf(
			"user A workspace was not empty after deletion: %+v",
			deletedUserAOpenAI,
		)
	}
}

func authenticatedUserRequest(
	testContext *testing.T,
	config runtimeconfig.Config,
	serverURL string,
	userID string,
	method string,
	path string,
	body string,
) *http.Response {
	testContext.Helper()
	request, requestError := http.NewRequest(
		method,
		serverURL+path,
		strings.NewReader(body),
	)
	if requestError != nil {
		testContext.Fatalf("create authenticated request: %v", requestError)
	}
	request.AddCookie(testSessionCookie(testContext, config, userID))
	if isMutationMethod(method) {
		request.Header.Set("Origin", config.Authentication().PublicOrigin())
		request.Header.Set(csrfHeaderName, config.CSRFToken())
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, responseError := http.DefaultClient.Do(request)
	if responseError != nil {
		testContext.Fatalf("perform authenticated request: %v", responseError)
	}
	return response
}

func importNetflixForUser(
	testContext *testing.T,
	config runtimeconfig.Config,
	serverURL string,
	userID string,
) string {
	testContext.Helper()
	createResponse := authenticatedUserRequest(
		testContext,
		config,
		serverURL,
		userID,
		http.MethodPost,
		netflixGenerationsPath,
		`{"analysis_level":"local"}`,
	)
	if createResponse.StatusCode != http.StatusCreated {
		testContext.Fatalf(
			"create %s Netflix generation status = %d; body=%s",
			userID,
			createResponse.StatusCode,
			readBody(testContext, createResponse),
		)
	}
	var created generationResponse
	decodeResponse(testContext, createResponse, &created)

	uploadRequest, requestError := http.NewRequest(
		http.MethodPut,
		serverURL+netflixGenerationsPath+"/"+created.Generation.ID+"/viewing-activity",
		strings.NewReader(httpSyntheticViewingCSV),
	)
	if requestError != nil {
		testContext.Fatalf("create %s Netflix upload request: %v", userID, requestError)
	}
	uploadRequest.AddCookie(testSessionCookie(testContext, config, userID))
	uploadRequest.Header.Set("Origin", config.Authentication().PublicOrigin())
	uploadRequest.Header.Set(csrfHeaderName, config.CSRFToken())
	uploadRequest.Header.Set("Content-Type", "text/csv")
	uploadResponse, uploadError := http.DefaultClient.Do(uploadRequest)
	if uploadError != nil {
		testContext.Fatalf("upload %s Netflix CSV: %v", userID, uploadError)
	}
	uploadResponse.Body.Close()
	if uploadResponse.StatusCode != http.StatusAccepted {
		testContext.Fatalf(
			"upload %s Netflix CSV status = %d; want %d",
			userID,
			uploadResponse.StatusCode,
			http.StatusAccepted,
		)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := requestNetflixSnapshotForUser(
			testContext,
			config,
			serverURL,
			userID,
		)
		if snapshot.Active != nil &&
			snapshot.Active.ID == created.Generation.ID &&
			snapshot.Active.State == netflixlibrary.GenerationStateReady {
			return created.Generation.ID
		}
		time.Sleep(5 * time.Millisecond)
	}
	testContext.Fatalf("timed out waiting for %s Netflix generation", userID)
	return ""
}

func requestNetflixSnapshotForUser(
	testContext *testing.T,
	config runtimeconfig.Config,
	serverURL string,
	userID string,
) netflixlibrary.Snapshot {
	testContext.Helper()
	response := authenticatedUserRequest(
		testContext,
		config,
		serverURL,
		userID,
		http.MethodGet,
		netflixProviderPath,
		"",
	)
	if response.StatusCode != http.StatusOK {
		testContext.Fatalf(
			"request %s Netflix snapshot status = %d; body=%s",
			userID,
			response.StatusCode,
			readBody(testContext, response),
		)
	}
	var snapshot netflixlibrary.Snapshot
	decodeResponse(testContext, response, &snapshot)
	return snapshot
}

func seedOpenAIArchiveForUser(
	testContext *testing.T,
	config runtimeconfig.Config,
	userID string,
) {
	testContext.Helper()
	userWorkspace := testUserWorkspace(testContext, config, userID)
	openedStore, openError := store.Open(userWorkspace.ArchiveDatabase())
	if openError != nil {
		testContext.Fatalf("open %s OpenAI archive: %v", userID, openError)
	}
	defer openedStore.Close()
	importer := ingest.Importer{Store: openedStore}
	sourcePath := filepath.Join("testdata", "synthetic-openai-export.zip")
	if _, importError := importer.Import(
		context.Background(),
		sourcePath,
		false,
	); importError != nil {
		testContext.Fatalf("import %s OpenAI archive: %v", userID, importError)
	}
}

func requestOpenAISnapshotForUser(
	testContext *testing.T,
	config runtimeconfig.Config,
	serverURL string,
	userID string,
) openAIProviderSnapshot {
	testContext.Helper()
	response := authenticatedUserRequest(
		testContext,
		config,
		serverURL,
		userID,
		http.MethodGet,
		openAIProviderPath,
		"",
	)
	if response.StatusCode != http.StatusOK {
		testContext.Fatalf(
			"request %s OpenAI snapshot status = %d; body=%s",
			userID,
			response.StatusCode,
			readBody(testContext, response),
		)
	}
	var snapshot openAIProviderSnapshot
	if decodeError := json.NewDecoder(response.Body).Decode(&snapshot); decodeError != nil {
		response.Body.Close()
		testContext.Fatalf("decode %s OpenAI snapshot: %v", userID, decodeError)
	}
	response.Body.Close()
	return snapshot
}
