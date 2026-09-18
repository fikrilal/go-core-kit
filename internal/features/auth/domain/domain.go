// Package domain holds the auth feature's framework-free types and helpers.
package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
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

// userAgentMaxLength mirrors the reference kit's session metadata policy.
const userAgentMaxLength = 512

// ErrEmailExists reports an email uniqueness violation.
var ErrEmailExists = errors.New("auth: email already exists")

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
