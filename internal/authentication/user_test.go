package authentication_test

import (
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/authentication"
)

func TestAuthenticatedUserPreservesIdentityAndDerivesOpaqueStorageID(
	testContext *testing.T,
) {
	user, userError := authentication.NewAuthenticatedUser(
		"download-your-data",
		"user-123",
	)
	if userError != nil {
		testContext.Fatalf("create authenticated user: %v", userError)
	}
	if user.TenantID() != "download-your-data" || user.UserID() != "user-123" {
		testContext.Fatalf("authenticated user changed source identity")
	}
	if len(user.StorageID()) != 64 ||
		strings.Contains(user.StorageID(), user.TenantID()) ||
		strings.Contains(user.StorageID(), user.UserID()) {
		testContext.Fatalf("storage identity is not an opaque SHA-256 digest: %q", user.StorageID())
	}
	if validationError := user.Validate(); validationError != nil {
		testContext.Fatalf("validate authenticated user: %v", validationError)
	}

	sameUser, sameUserError := authentication.NewAuthenticatedUser(
		"download-your-data",
		"user-123",
	)
	if sameUserError != nil {
		testContext.Fatalf("recreate authenticated user: %v", sameUserError)
	}
	if sameUser.StorageID() != user.StorageID() {
		testContext.Fatalf("stable identity produced different storage digests")
	}
	otherTenant, otherTenantError := authentication.NewAuthenticatedUser(
		"another-tenant",
		"user-123",
	)
	if otherTenantError != nil {
		testContext.Fatalf("create other-tenant user: %v", otherTenantError)
	}
	if otherTenant.StorageID() == user.StorageID() {
		testContext.Fatalf("tenant identity did not participate in storage ownership")
	}
}

func TestAuthenticatedUserRejectsNonCanonicalIdentity(testContext *testing.T) {
	testCases := []struct {
		name     string
		tenantID string
		userID   string
	}{
		{name: "missing tenant", tenantID: "", userID: "user"},
		{name: "missing user", tenantID: "tenant", userID: ""},
		{name: "trimmed tenant", tenantID: " tenant", userID: "user"},
		{name: "trimmed user", tenantID: "tenant", userID: "user "},
		{name: "control character", tenantID: "tenant", userID: "user\nother"},
		{name: "oversized user", tenantID: "tenant", userID: strings.Repeat("x", 513)},
	}
	for _, testCase := range testCases {
		testContext.Run(testCase.name, func(testContext *testing.T) {
			if _, userError := authentication.NewAuthenticatedUser(
				testCase.tenantID,
				testCase.userID,
			); userError == nil {
				testContext.Fatalf("invalid authenticated user was accepted")
			}
		})
	}
}

func TestAuthenticatedUserRejectsZeroValue(testContext *testing.T) {
	if validationError := (authentication.AuthenticatedUser{}).Validate(); validationError == nil {
		testContext.Fatalf("zero authenticated user was accepted")
	}
}
