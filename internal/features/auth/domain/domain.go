// Package domain holds the auth feature's framework-free types and helpers.
package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Roles.
const (
	RoleUser  = "USER"
	RoleAdmin = "ADMIN"
)

// User statuses.
const (
	StatusActive    = "ACTIVE"
	StatusSuspended = "SUSPENDED"
	StatusDeleted   = "DELETED"
)

// AuthMethodPassword names the password authentication method.
const AuthMethodPassword = "PASSWORD"

// AuthMethodGoogle names the Google OIDC authentication method.
const AuthMethodGoogle = "GOOGLE"

// userAgentMaxLength mirrors the reference kit's session metadata policy.
const userAgentMaxLength = 512

// ErrEmailExists reports an email uniqueness violation.
var ErrEmailExists = errors.New("auth: email already exists")

// ErrExternalIdentityExists reports an identity uniqueness violation.
var ErrExternalIdentityExists = errors.New("auth: external identity already exists")

// User is the authenticated account core.
type User struct {
	ID            string
	Email         string
	EmailVerified bool
	Role          string
	Status        string
}

// LoginUser pairs a user with the stored password hash when one exists.
type LoginUser struct {
	User         User
	PasswordHash *string
}

// LoginSession is the session core a refresh token belongs to.
type LoginSession struct {
	ID        string
	ExpiresAt time.Time
	Revoked   bool
}

// RefreshToken is the stored state of a presented refresh token.
type RefreshToken struct {
	ID           string
	ExpiresAt    time.Time
	Revoked      bool
	ReplacedByID *string
}

// RefreshCredentials pairs a presented refresh token with its session and user.
type RefreshCredentials struct {
	User    User
	Session LoginSession
	Token   RefreshToken
}

// RotationKind reports how a refresh rotation ended.
type RotationKind string

// Rotation outcomes.
const (
	RotationOK             RotationKind = "ok"
	RotationNotFound       RotationKind = "not_found"
	RotationExpired        RotationKind = "expired"
	RotationSessionRevoked RotationKind = "session_revoked"
	RotationReused         RotationKind = "reused"
)

// ChangePasswordKind reports how a password change ended.
type ChangePasswordKind string

// Password change outcomes.
const (
	ChangePasswordOK                      ChangePasswordKind = "ok"
	ChangePasswordNotFound                ChangePasswordKind = "not_found"
	ChangePasswordPasswordNotSet          ChangePasswordKind = "password_not_set"
	ChangePasswordCurrentPasswordMismatch ChangePasswordKind = "current_password_mismatch"
)

// LinkIdentityKind reports how linking an OIDC identity ended.
type LinkIdentityKind string

// Identity link outcomes.
const (
	LinkIdentityOK                    LinkIdentityKind = "ok"
	LinkIdentityAlreadyLinked         LinkIdentityKind = "already_linked"
	LinkIdentityUserNotFound          LinkIdentityKind = "user_not_found"
	LinkIdentityOwnedByOtherUser      LinkIdentityKind = "owned_by_other_user"
	LinkIdentityProviderAlreadyLinked LinkIdentityKind = "provider_already_linked"
)

// Profile is the display profile attached to a user.
type Profile struct {
	DisplayName string
	GivenName   string
	FamilyName  string
}

// MeView is the composed Me response core: user, profile, and auth methods.
type MeView struct {
	User        User
	Profile     Profile
	AuthMethods []string
}

// OidcVerifyKind reports how an OIDC token verification ended.
type OidcVerifyKind string

// OIDC verification kinds.
const (
	OidcVerifyVerified      OidcVerifyKind = "verified"
	OidcVerifyInvalid       OidcVerifyKind = "invalid"
	OidcVerifyNotConfigured OidcVerifyKind = "not_configured"
)

// OidcIdentity is a verified external identity.
type OidcIdentity struct {
	Provider      string
	Subject       string
	Email         string
	EmailVerified bool
	DisplayName   string
	GivenName     string
	FamilyName    string
}

// OidcVerification pairs a verification kind with the identity when verified.
type OidcVerification struct {
	Kind     OidcVerifyKind
	Identity OidcIdentity
}

// NormalizeEmail trims and lowercases an email for storage and lookup.
func NormalizeEmail(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// NewRefreshToken returns a fresh opaque refresh token.
func NewRefreshToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("auth: read refresh token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

// HashRefreshToken returns the storage hash for a refresh token.
func HashRefreshToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// ActiveSessionKey returns the single-active-session key for a device.
func ActiveSessionKey(userID, deviceID string) (string, bool) {
	if deviceID == "" {
		return "", false
	}
	return userID + ":" + deviceID, true
}

// NormalizeUserAgent trims and caps session user-agent metadata.
func NormalizeUserAgent(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}
	if len(trimmed) > userAgentMaxLength {
		trimmed = trimmed[:userAgentMaxLength]
	}
	return trimmed, true
}
