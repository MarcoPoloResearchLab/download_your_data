package httpapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/authentication"
	"github.com/MarcoPoloResearchLab/download_your_data/internal/runtimeconfig"
	"gopkg.in/yaml.v3"
)

const (
	uiConfigPath                    = "/config-ui.yaml"
	userWorkspacePath               = "/api/workspace"
	userWorkspaceDeleteConfirmation = "delete-download-your-data-workspace"
)

var allowedCORSMethods = []string{
	http.MethodGet,
	http.MethodHead,
	http.MethodPost,
	http.MethodPut,
	http.MethodDelete,
}

type uiConfigDocument struct {
	Environments []uiConfigEnvironment `yaml:"environments"`
}

type uiConfigEnvironment struct {
	Description string       `yaml:"description"`
	Origins     []string     `yaml:"origins"`
	Auth        uiConfigAuth `yaml:"auth"`
}

type uiConfigAuth struct {
	TAuthURL       string `yaml:"tauthUrl"`
	GoogleClientID string `yaml:"googleClientId"`
	TenantID       string `yaml:"tenantId"`
	LoginPath      string `yaml:"loginPath"`
	LogoutPath     string `yaml:"logoutPath"`
	NoncePath      string `yaml:"noncePath"`
	SessionPath    string `yaml:"sessionPath"`
}

type deleteUserWorkspaceRequest struct {
	Confirmation string `json:"confirmation"`
}

func writeUIConfig(
	config runtimeconfig.Config,
	logger *slog.Logger,
) http.HandlerFunc {
	document := uiConfigDocument{
		Environments: []uiConfigEnvironment{{
			Description: "Download Your Data",
			Origins:     []string{config.Authentication().PublicOrigin()},
			Auth: uiConfigAuth{
				TAuthURL:       config.Authentication().TAuthURL(),
				GoogleClientID: config.Authentication().GoogleClientID(),
				TenantID:       config.Authentication().TenantID(),
				LoginPath:      runtimeconfig.TAuthLoginPath,
				LogoutPath:     runtimeconfig.TAuthLogoutPath,
				NoncePath:      runtimeconfig.TAuthNoncePath,
				SessionPath:    runtimeconfig.TAuthSessionPath,
			},
		}},
	}
	encodedDocument, encodeError := yaml.Marshal(document)
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		if encodeError != nil || len(encodedDocument) > runtimeconfig.MaximumBrowserConfigurationBytes() {
			logger.Error(
				"write browser configuration",
				"error_type",
				"browser_configuration_encoding_failed",
			)
			writeRequestError(responseWriter, http.StatusInternalServerError, "internal_error")
			return
		}
		if queryError := requireQueryKeys(request, nil); queryError != nil {
			writeRequestError(responseWriter, http.StatusBadRequest, "invalid_query")
			return
		}
		responseWriter.Header().Set("Cache-Control", "no-store")
		responseWriter.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		responseWriter.WriteHeader(http.StatusOK)
		if _, writeError := responseWriter.Write(encodedDocument); writeError != nil {
			logger.Error(
				"write browser configuration",
				"error_type",
				"browser_configuration_write_failed",
			)
		}
	}
}

func requireAuthenticatedUser(
	boundary *authentication.Boundary,
	coordinator *userRequestCoordinator,
	next http.Handler,
	logger *slog.Logger,
) http.Handler {
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		user, authenticationError := boundary.Authenticate(request)
		if authenticationError != nil {
			logger.Info(
				"authentication boundary rejected request",
				"error_type",
				"session_required",
				"method",
				request.Method,
				"path",
				request.URL.EscapedPath(),
			)
			writeRequestError(responseWriter, http.StatusUnauthorized, "session_required")
			return
		}
		authenticatedRequest, injectionError := authentication.WithUser(request, user)
		if injectionError != nil {
			logger.Error(
				"authentication boundary failed",
				"error_type",
				"authenticated_user_injection_failed",
			)
			writeRequestError(responseWriter, http.StatusInternalServerError, "internal_error")
			return
		}
		operation := func() {
			next.ServeHTTP(responseWriter, authenticatedRequest)
		}
		if request.Method == http.MethodDelete && request.URL.Path == userWorkspacePath {
			coordinator.exclusive(user, operation)
			return
		}
		coordinator.shared(user, operation)
	})
}

func deleteAuthenticatedWorkspace(
	registry *netflixWorkspaceRegistry,
	logger *slog.Logger,
) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		var payload deleteUserWorkspaceRequest
		if decodeError := decodeJSONRequest(responseWriter, request, &payload); decodeError != nil {
			writeJSONRequestError(responseWriter, decodeError)
			return
		}
		if payload.Confirmation != userWorkspaceDeleteConfirmation {
			writeRequestError(
				responseWriter,
				http.StatusUnprocessableEntity,
				"invalid_deletion_confirmation",
			)
			return
		}
		user, userError := authentication.UserFromRequest(request)
		if userError != nil {
			logger.Error(
				"delete user workspace failed",
				"error_type",
				"authenticated_user_unavailable",
			)
			writeRequestError(responseWriter, http.StatusInternalServerError, "internal_error")
			return
		}
		if deleteError := registry.deleteUser(user); deleteError != nil {
			if errors.Is(deleteError, errWorkspaceInUse) {
				writeRequestError(responseWriter, http.StatusConflict, "workspace_in_use")
				return
			}
			logger.Error(
				"delete user workspace failed",
				"error_type",
				"workspace_deletion_failed",
			)
			writeRequestError(responseWriter, http.StatusInternalServerError, "internal_error")
			return
		}
		writeNoContent(responseWriter)
	}
}

func applyRequestBoundary(
	config runtimeconfig.Config,
	next http.Handler,
) http.Handler {
	allowedOrigin := config.Authentication().PublicOrigin()
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		origin := request.Header.Get("Origin")
		if origin != "" {
			if origin != allowedOrigin {
				writeRequestError(responseWriter, http.StatusForbidden, "invalid_origin")
				return
			}
			setCredentialedCORSHeaders(responseWriter, allowedOrigin)
		}
		if request.Method == http.MethodOptions {
			if origin == "" ||
				!validPreflightMethod(request.Header.Get("Access-Control-Request-Method")) ||
				!validPreflightHeaders(request.Header.Get("Access-Control-Request-Headers")) {
				writeRequestError(responseWriter, http.StatusForbidden, "invalid_preflight")
				return
			}
			responseWriter.Header().Set(
				"Access-Control-Allow-Methods",
				strings.Join(allowedCORSMethods, ", "),
			)
			responseWriter.Header().Set(
				"Access-Control-Allow-Headers",
				"Content-Type, "+csrfHeaderName,
			)
			responseWriter.Header().Set("Access-Control-Max-Age", "600")
			responseWriter.WriteHeader(http.StatusNoContent)
			return
		}
		if isMutationMethod(request.Method) {
			if origin != allowedOrigin {
				writeRequestError(responseWriter, http.StatusForbidden, "invalid_origin")
				return
			}
			if request.Header.Get(csrfHeaderName) != config.CSRFToken() {
				writeRequestError(responseWriter, http.StatusForbidden, "invalid_csrf_token")
				return
			}
		}
		next.ServeHTTP(responseWriter, request)
	})
}

func setCredentialedCORSHeaders(responseWriter http.ResponseWriter, origin string) {
	responseWriter.Header().Set("Access-Control-Allow-Origin", origin)
	responseWriter.Header().Set("Access-Control-Allow-Credentials", "true")
	responseWriter.Header().Add("Vary", "Origin")
}

func validPreflightMethod(method string) bool {
	return slices.Contains(allowedCORSMethods, method)
}

func validPreflightHeaders(rawHeaders string) bool {
	if strings.TrimSpace(rawHeaders) == "" {
		return true
	}
	for _, rawHeader := range strings.Split(rawHeaders, ",") {
		header := strings.ToLower(strings.TrimSpace(rawHeader))
		if header != "content-type" && header != strings.ToLower(csrfHeaderName) {
			return false
		}
	}
	return true
}

func buildContentSecurityPolicy(config runtimeconfig.Config) string {
	return fmt.Sprintf(
		"default-src 'self'; base-uri 'self'; connect-src 'self' %s %s https://accounts.google.com; font-src 'self'; form-action 'self' %s; frame-ancestors 'none'; frame-src https://accounts.google.com; img-src 'self' data: https://lh3.googleusercontent.com; object-src 'none'; script-src 'self' https://cdn.jsdelivr.net https://accounts.google.com; style-src 'self' https://cdn.jsdelivr.net 'unsafe-inline'",
		config.Authentication().APIOrigin(),
		config.Authentication().TAuthURL(),
		config.Authentication().TAuthURL(),
	)
}
