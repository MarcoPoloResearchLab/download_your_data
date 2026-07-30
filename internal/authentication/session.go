package authentication

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/tyemirov/tauth/pkg/sessionvalidator"
)

var (
	// ErrUnauthenticated is the non-sensitive identity for an absent, invalid,
	// foreign-tenant, or incomplete TAuth session.
	ErrUnauthenticated = errors.New("authentication.session_required")
	// ErrUserUnavailable identifies a protected service call whose HTTP
	// boundary did not inject an authenticated user.
	ErrUserUnavailable = errors.New("authentication.user_unavailable")
)

type requestUserKey struct{}

// SessionValidator is the published TAuth request-validation surface used by
// the application authorization boundary.
type SessionValidator interface {
	ValidateRequest(request *http.Request) (*sessionvalidator.Claims, error)
}

// Boundary converts validated TAuth claims into the sole application user
// value.
type Boundary struct {
	validator SessionValidator
	tenantID  string
}

// NewBoundary creates the fail-closed TAuth authorization boundary.
func NewBoundary(
	validator SessionValidator,
	tenantID string,
) (*Boundary, error) {
	if validator == nil {
		return nil, errors.New("create authentication boundary: session validator is required")
	}
	validatedTenant, tenantError := NewAuthenticatedUser(tenantID, "configuration-probe")
	if tenantError != nil {
		return nil, fmt.Errorf("create authentication boundary: %w", tenantError)
	}
	return &Boundary{
		validator: validator,
		tenantID:  validatedTenant.TenantID(),
	}, nil
}

// Authenticate validates the request's TAuth session and returns its immutable
// resource owner.
func (boundary *Boundary) Authenticate(
	request *http.Request,
) (AuthenticatedUser, error) {
	if boundary == nil || boundary.validator == nil || request == nil {
		return AuthenticatedUser{}, ErrUnauthenticated
	}
	claims, validationError := boundary.validator.ValidateRequest(request)
	if validationError != nil || claims == nil || claims.GetTenantID() != boundary.tenantID {
		return AuthenticatedUser{}, ErrUnauthenticated
	}
	user, userError := NewAuthenticatedUser(claims.GetTenantID(), claims.GetUserID())
	if userError != nil {
		return AuthenticatedUser{}, ErrUnauthenticated
	}
	return user, nil
}

// WithUser injects a validated user into a protected request.
func WithUser(
	request *http.Request,
	user AuthenticatedUser,
) (*http.Request, error) {
	if request == nil {
		return nil, errors.New("inject authenticated user: request is required")
	}
	if validationError := user.Validate(); validationError != nil {
		return nil, fmt.Errorf("inject authenticated user: %w", validationError)
	}
	return request.WithContext(
		context.WithValue(request.Context(), requestUserKey{}, user),
	), nil
}

// UserFromRequest returns the immutable user injected by the protected HTTP
// boundary.
func UserFromRequest(request *http.Request) (AuthenticatedUser, error) {
	if request == nil {
		return AuthenticatedUser{}, ErrUserUnavailable
	}
	user, present := request.Context().Value(requestUserKey{}).(AuthenticatedUser)
	if !present || user.Validate() != nil {
		return AuthenticatedUser{}, ErrUserUnavailable
	}
	return user, nil
}
