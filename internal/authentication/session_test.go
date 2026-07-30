package authentication_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/authentication"
	"github.com/tyemirov/tauth/pkg/sessionvalidator"
)

type fixtureSessionValidator struct {
	claims *sessionvalidator.Claims
	err    error
}

func (validator fixtureSessionValidator) ValidateRequest(
	*http.Request,
) (*sessionvalidator.Claims, error) {
	return validator.claims, validator.err
}

func TestBoundaryConvertsOnlyTheConfiguredTAuthTenant(testContext *testing.T) {
	boundary, boundaryError := authentication.NewBoundary(
		fixtureSessionValidator{claims: &sessionvalidator.Claims{
			TenantID: "download-your-data",
			UserID:   "user-123",
		}},
		"download-your-data",
	)
	if boundaryError != nil {
		testContext.Fatalf("create authentication boundary: %v", boundaryError)
	}
	user, authenticationError := boundary.Authenticate(
		httptest.NewRequest(http.MethodGet, "https://api.example.test/protected", nil),
	)
	if authenticationError != nil {
		testContext.Fatalf("authenticate request: %v", authenticationError)
	}
	if user.TenantID() != "download-your-data" || user.UserID() != "user-123" {
		testContext.Fatalf("unexpected authenticated user")
	}
	request, injectionError := authentication.WithUser(
		httptest.NewRequest(http.MethodGet, "https://api.example.test/protected", nil),
		user,
	)
	if injectionError != nil {
		testContext.Fatalf("inject authenticated user: %v", injectionError)
	}
	resolved, resolveError := authentication.UserFromRequest(request)
	if resolveError != nil {
		testContext.Fatalf("resolve authenticated user: %v", resolveError)
	}
	if resolved != user {
		testContext.Fatalf("request user changed across the HTTP boundary")
	}
}

func TestBoundaryRejectsEveryInvalidSessionShape(testContext *testing.T) {
	testCases := []struct {
		name      string
		validator authentication.SessionValidator
	}{
		{
			name:      "validator error",
			validator: fixtureSessionValidator{err: errors.New("invalid token")},
		},
		{
			name:      "missing claims",
			validator: fixtureSessionValidator{},
		},
		{
			name: "foreign tenant",
			validator: fixtureSessionValidator{claims: &sessionvalidator.Claims{
				TenantID: "foreign",
				UserID:   "user-123",
			}},
		},
		{
			name: "missing user",
			validator: fixtureSessionValidator{claims: &sessionvalidator.Claims{
				TenantID: "download-your-data",
			}},
		},
	}
	for _, testCase := range testCases {
		testContext.Run(testCase.name, func(testContext *testing.T) {
			boundary, boundaryError := authentication.NewBoundary(
				testCase.validator,
				"download-your-data",
			)
			if boundaryError != nil {
				testContext.Fatalf("create authentication boundary: %v", boundaryError)
			}
			if _, authenticationError := boundary.Authenticate(
				httptest.NewRequest(http.MethodGet, "https://api.example.test", nil),
			); !errors.Is(authenticationError, authentication.ErrUnauthenticated) {
				testContext.Fatalf("authentication error = %v; want session required", authenticationError)
			}
		})
	}
}

func TestBoundaryRejectsInvalidConstructionAndMissingRequestUser(
	testContext *testing.T,
) {
	if _, boundaryError := authentication.NewBoundary(nil, "tenant"); boundaryError == nil {
		testContext.Fatalf("nil session validator was accepted")
	}
	if _, boundaryError := authentication.NewBoundary(
		fixtureSessionValidator{},
		" tenant",
	); boundaryError == nil {
		testContext.Fatalf("invalid configured tenant was accepted")
	}
	request := httptest.NewRequest(http.MethodGet, "https://api.example.test", nil)
	if _, userError := authentication.UserFromRequest(request); !errors.Is(
		userError,
		authentication.ErrUserUnavailable,
	) {
		testContext.Fatalf("missing request user error = %v", userError)
	}
	if _, injectionError := authentication.WithUser(
		request,
		authentication.AuthenticatedUser{},
	); injectionError == nil {
		testContext.Fatalf("zero user was injected")
	}
}
