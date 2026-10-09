// Package domain holds the me feature's framework-free types.
package domain

import "time"

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

// Auth methods.
const (
	AuthMethodPassword = "PASSWORD"
	AuthMethodGoogle   = "GOOGLE"
)

// User is the account core the Me view carries.
type User struct {
	ID            string
	Email         string
	EmailVerified bool
	Role          string
	Status        string
}

// Profile is the display profile attached to a user.
type Profile struct {
	DisplayName string
	GivenName   string
	FamilyName  string
}

// MeView is the composed Me response core.
type MeView struct {
	User        User
	Profile     Profile
	AuthMethods []string
}

// Session statuses.
const (
	SessionActive  = "active"
	SessionRevoked = "revoked"
	SessionExpired = "expired"
)

// Session is the stored session core. RevokedAt is zero when not revoked.
type Session struct {
	ID         string
	DeviceID   string
	DeviceName string
	IP         string
	UserAgent  string
	LastSeenAt time.Time
	CreatedAt  time.Time
	ExpiresAt  time.Time
	RevokedAt  time.Time
}

// SessionView adds the computed status and the current-session flag.
type SessionView struct {
	Session
	Current bool
	Status  string
}

// Field is a patch field that can be absent, cleared (null), or set.
type Field struct {
	Present bool
	Null    bool
	Value   string
}

// ProfilePatch carries the profile fields a patch request defined.
type ProfilePatch struct {
	DisplayName Field
	GivenName   Field
	FamilyName  Field
}

// Defined reports whether the patch carries at least one profile field.
func (p ProfilePatch) Defined() bool {
	return p.DisplayName.Present || p.GivenName.Present || p.FamilyName.Present
}
