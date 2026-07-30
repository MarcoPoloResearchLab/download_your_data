// Package authentication owns the validated user identity established at the
// TAuth session boundary.
package authentication

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxIdentityBytes       = 512
	storageIdentityVersion = "download-your-data-user-v1"
)

// AuthenticatedUser is the immutable owner identity for every protected
// application resource. Profile fields such as email and display name are
// deliberately excluded.
type AuthenticatedUser struct {
	tenantID  string
	userID    string
	storageID string
}

// NewAuthenticatedUser validates the exact tenant and user identifiers from a
// TAuth session.
func NewAuthenticatedUser(tenantID string, userID string) (AuthenticatedUser, error) {
	validatedTenantID, tenantError := validateIdentityPart("tenant ID", tenantID)
	if tenantError != nil {
		return AuthenticatedUser{}, tenantError
	}
	validatedUserID, userError := validateIdentityPart("user ID", userID)
	if userError != nil {
		return AuthenticatedUser{}, userError
	}
	storageDigest := sha256.Sum256([]byte(
		storageIdentityVersion + "\x00" + validatedTenantID + "\x00" + validatedUserID,
	))
	return AuthenticatedUser{
		tenantID:  validatedTenantID,
		userID:    validatedUserID,
		storageID: hex.EncodeToString(storageDigest[:]),
	}, nil
}

// TenantID returns the exact validated TAuth tenant identifier.
func (user AuthenticatedUser) TenantID() string {
	return user.tenantID
}

// UserID returns the exact validated TAuth user identifier.
func (user AuthenticatedUser) UserID() string {
	return user.userID
}

// StorageID returns the stable, non-secret digest used for the user's private
// directory name.
func (user AuthenticatedUser) StorageID() string {
	return user.storageID
}

// Validate reports whether the value was created by NewAuthenticatedUser.
func (user AuthenticatedUser) Validate() error {
	recreated, recreationError := NewAuthenticatedUser(user.tenantID, user.userID)
	if recreationError != nil {
		return recreationError
	}
	if user.storageID == "" || user.storageID != recreated.storageID {
		return errors.New("validate authenticated user: storage identity is invalid")
	}
	return nil
}

func validateIdentityPart(label string, value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("validate authenticated user %s: value is required", label)
	}
	if value != strings.TrimSpace(value) {
		return "", fmt.Errorf(
			"validate authenticated user %s: value must not have surrounding whitespace",
			label,
		)
	}
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("validate authenticated user %s: value must be UTF-8", label)
	}
	if len([]byte(value)) > maxIdentityBytes {
		return "", fmt.Errorf(
			"validate authenticated user %s: value exceeds %d bytes",
			label,
			maxIdentityBytes,
		)
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", fmt.Errorf(
				"validate authenticated user %s: control characters are not allowed",
				label,
			)
		}
	}
	return value, nil
}
