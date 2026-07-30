// Command test-session-token mints a short-lived TAuth session exclusively for
// repository-owned browser contract tests.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/tyemirov/tauth/pkg/sessionvalidator"
)

func main() {
	signingKey := flag.String("signing-key", "", "test-only HS256 signing key")
	tenantID := flag.String("tenant-id", "", "test tenant identifier")
	userID := flag.String("user-id", "", "test user identifier")
	flag.Parse()

	if flag.NArg() != 0 ||
		len([]byte(*signingKey)) < 32 ||
		*tenantID == "" ||
		*userID == "" {
		fmt.Fprintln(os.Stderr, "test-session-token requires a 32-byte key, tenant, and user")
		os.Exit(2)
	}
	now := time.Now().UTC()
	claims := sessionvalidator.Claims{
		TenantID:        *tenantID,
		UserID:          *userID,
		UserEmail:       "browser-contract@example.invalid",
		UserDisplayName: "Browser Contract",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "tauth",
			Subject:   *userID,
			IssuedAt:  jwt.NewNumericDate(now.Add(-time.Minute)),
			NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
			ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
		},
	}
	signedToken, signingError := jwt.NewWithClaims(
		jwt.SigningMethodHS256,
		claims,
	).SignedString([]byte(*signingKey))
	if signingError != nil {
		fmt.Fprintln(os.Stderr, "mint browser contract session")
		os.Exit(1)
	}
	fmt.Print(signedToken)
}
