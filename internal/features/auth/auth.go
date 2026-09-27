//go:build !pg

// Package auth implements the auth feature's HTTP surface and wiring.
// This build is SQLite until a pg repository exists.
package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/fikrilal/go-core-kit/contracts"
	"github.com/fikrilal/go-core-kit/internal/features/auth/app"
	"github.com/fikrilal/go-core-kit/internal/features/auth/domain"
	"github.com/fikrilal/go-core-kit/internal/features/auth/infra"
	platformauth "github.com/fikrilal/go-core-kit/internal/platform/auth"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
	"github.com/fikrilal/go-core-kit/internal/platform/password"
	"github.com/fikrilal/go-core-kit/internal/platform/redis"
)

const dummyPassword = "dummy-password-for-timing"

// Config carries the auth policy knobs from platform configuration.
type Config struct {
	AccessTokenTTL        time.Duration
	SessionTTL            time.Duration
	PasswordMinLength     int
	LoginMaxAttempts      int
	LoginWindow           time.Duration
	LoginBlock            time.Duration
	Issuer                string
	Audience              string
	SigningKeysJSON       string
	SigningKeysJSONBase64 string
	ProductionLike        bool
	OIDCGoogleClientIDs   string
	OIDCGoogleJWKSURL     string
}

// Deps are the auth feature's dependencies.
type Deps struct {
	Database *db.Database
	Redis    *redis.Client
	Clock    clock.Clock
	Config   Config
}

// Register mounts the auth feature on the router.
func Register(router *httpx.Router, deps Deps) error {
	keyRing, err := platformauth.NewKeyRing(platformauth.KeyRingOptions{
		KeysJSON:       deps.Config.SigningKeysJSON,
		KeysJSONBase64: deps.Config.SigningKeysJSONBase64,
		ProductionLike: deps.Config.ProductionLike,
	})
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	hasher := password.New()
	dummyHash, err := hasher.Hash(dummyPassword)
	if err != nil {
		return fmt.Errorf("auth: build dummy hash: %w", err)
	}
	service := app.New(
		app.Config{
			PasswordMinLength: deps.Config.PasswordMinLength,
			SessionTTL:        deps.Config.SessionTTL,
		},
		app.Deps{
			Repository: infra.NewRepository(deps.Database),
			Hasher:     hasher,
			Tokens: issuerAdapter{issuer: platformauth.NewIssuer(
				keyRing, deps.Clock, deps.Config.Issuer, deps.Config.Audience, deps.Config.AccessTokenTTL,
			)},
			Limiter:   infra.NewLoginLimiter(deps.Redis, deps.Config.LoginMaxAttempts, deps.Config.LoginWindow, deps.Config.LoginBlock),
			Oidc:      infra.NewGoogleIDTokenVerifier(deps.Config.OIDCGoogleClientIDs, deps.Config.OIDCGoogleJWKSURL, deps.Clock),
			Clock:     deps.Clock,
			DummyHash: dummyHash,
		},
	)
	verifier, err := platformauth.NewVerifier(keyRing, deps.Clock, deps.Config.Issuer, deps.Config.Audience, deps.Config.ProductionLike)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	guard := platformauth.NewGuard(verifier)
	router.SetIdempotencyCaller(idempotencyCaller)
	handlers := &handlers{service: service, keys: keyRing}
	router.HandleFunc(httpx.Operation{Method: http.MethodPost, Path: "/v1/auth/password/register", ID: "auth.password.register"}, handlers.register)
	router.HandleFunc(httpx.Operation{Method: http.MethodPost, Path: "/v1/auth/password/login", ID: "auth.password.login"}, handlers.login)
	router.HandleFunc(httpx.Operation{Method: http.MethodPost, Path: "/v1/auth/refresh", ID: "auth.refresh"}, handlers.refresh)
	router.HandleFunc(httpx.Operation{Method: http.MethodPost, Path: "/v1/auth/logout", ID: "auth.logout"}, handlers.logout)
	router.HandleFunc(httpx.Operation{Method: http.MethodGet, Path: "/.well-known/jwks.json", ID: "auth.jwks.get"}, handlers.jwks)
	router.HandleFunc(httpx.Operation{Method: http.MethodPost, Path: "/v1/auth/oidc/exchange", ID: "auth.oidc.exchange"}, handlers.exchangeOidc)
	changePassword := httpx.Operation{Method: http.MethodPost, Path: "/v1/auth/password/change", ID: "auth.password.change"}
	router.Handle(changePassword, guard.Require(router.Idempotent(changePassword, handlers.changePassword)))
	return nil
}

func idempotencyCaller(r *http.Request) (string, bool) {
	principal, ok := platformauth.PrincipalFrom(r.Context())
	if !ok || principal.UserID == "" {
		return "", false
	}
	return principal.UserID, true
}

type issuerAdapter struct {
	issuer *platformauth.Issuer
}

func (a issuerAdapter) SignAccessToken(user domain.User, sessionID string) (string, error) {
	token, err := a.issuer.SignAccessToken(platformauth.AccessClaims{
		UserID:        user.ID,
		SessionID:     sessionID,
		EmailVerified: user.EmailVerified,
		Roles:         []string{user.Role},
	})
	if err != nil {
		return "", fmt.Errorf("auth: sign access token: %w", err)
	}
	return token, nil
}

type handlers struct {
	service *app.Service
	keys    *platformauth.KeyRing
}

type passwordRequest struct {
	Email      string `json:"email" validate:"required,email"`
	Password   string `json:"password" validate:"required"`
	DeviceID   string `json:"deviceId"`
	DeviceName string `json:"deviceName"`
}

func (h *handlers) register(w http.ResponseWriter, r *http.Request) {
	h.password(w, r, h.service.Register)
}

func (h *handlers) login(w http.ResponseWriter, r *http.Request) {
	h.password(w, r, h.service.Login)
}

func (h *handlers) password(w http.ResponseWriter, r *http.Request, call func(context.Context, app.Input) (app.Result, error)) {
	var body passwordRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	result, err := call(r.Context(), app.Input{
		Email:      body.Email,
		Password:   body.Password,
		DeviceID:   body.DeviceID,
		DeviceName: body.DeviceName,
		IP:         clientIP(r),
		UserAgent:  r.Header.Get("User-Agent"),
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	h.respondAuth(w, r, result)
}

type oidcExchangeRequest struct {
	Provider   string `json:"provider" validate:"required,oneof=GOOGLE"`
	IDToken    string `json:"idToken" validate:"required"`
	DeviceID   string `json:"deviceId"`
	DeviceName string `json:"deviceName"`
}

func (h *handlers) exchangeOidc(w http.ResponseWriter, r *http.Request) {
	var body oidcExchangeRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	result, err := h.service.ExchangeOidc(r.Context(), app.OidcExchangeInput{
		Provider:   body.Provider,
		IDToken:    body.IDToken,
		DeviceID:   body.DeviceID,
		DeviceName: body.DeviceName,
		IP:         clientIP(r),
		UserAgent:  r.Header.Get("User-Agent"),
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	h.respondAuth(w, r, result)
}

// respondAuth writes the token pair. The second read is the Me profile and auth methods.
func (h *handlers) respondAuth(w http.ResponseWriter, r *http.Request, result app.Result) {
	me, err := h.service.MeView(r.Context(), result.User.ID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Data(w, r, authResult(result, me))
}

type refreshTokenRequest struct {
	RefreshToken string `json:"refreshToken" validate:"required"`
}

func (h *handlers) refresh(w http.ResponseWriter, r *http.Request) {
	var body refreshTokenRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	result, err := h.service.Refresh(r.Context(), app.RefreshInput{
		RefreshToken: body.RefreshToken,
		IP:           clientIP(r),
		UserAgent:    r.Header.Get("User-Agent"),
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Data(w, r, refreshResult(result))
}

func (h *handlers) logout(w http.ResponseWriter, r *http.Request) {
	var body refreshTokenRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.service.Logout(r.Context(), body.RefreshToken); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *handlers) jwks(w http.ResponseWriter, r *http.Request) {
	httpx.Document(w, r, h.keys.PublicJWKS())
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword" validate:"required"`
	NewPassword     string `json:"newPassword" validate:"required"`
}

func (h *handlers) changePassword(w http.ResponseWriter, r *http.Request) {
	principal, ok := platformauth.PrincipalFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, &httpx.ProblemError{Status: http.StatusUnauthorized, Code: httpx.CodeUnauthorized})
		return
	}
	var body changePasswordRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	err := h.service.ChangePassword(r.Context(), app.ChangePasswordInput{
		UserID:          principal.UserID,
		SessionID:       principal.SessionID,
		CurrentPassword: body.CurrentPassword,
		NewPassword:     body.NewPassword,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func authResult(result app.Result, me domain.MeView) contracts.AuthResultWithMeDto {
	methods := make([]contracts.MeDtoAuthMethods, 0, len(me.AuthMethods))
	for _, method := range me.AuthMethods {
		methods = append(methods, contracts.MeDtoAuthMethods(method))
	}
	return contracts.AuthResultWithMeDto{
		User: contracts.MeDto{
			Id:            me.User.ID,
			Email:         me.User.Email,
			EmailVerified: me.User.EmailVerified,
			Roles:         []string{me.User.Role},
			AuthMethods:   methods,
			Profile: contracts.MeProfileDto{
				DisplayName: nullableString(me.Profile.DisplayName),
				GivenName:   nullableString(me.Profile.GivenName),
				FamilyName:  nullableString(me.Profile.FamilyName),
			},
		},
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
	}
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// refreshResult renders the refreshed pair; authMethods is omitted on refresh responses.
func refreshResult(result app.Result) contracts.AuthResultDto {
	return contracts.AuthResultDto{
		User: contracts.AuthUserDto{
			Id:            result.User.ID,
			Email:         result.User.Email,
			EmailVerified: result.User.EmailVerified,
		},
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
