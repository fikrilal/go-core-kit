//go:build !pg

// Package auth implements the auth feature's HTTP surface and wiring.
package auth

import (
	"context"
	"fmt"
	"log/slog"
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
}

// Deps are the auth feature's dependencies.
type Deps struct {
	Database *db.Database
	Redis    *redis.Client
	Logger   *slog.Logger
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
			Clock:     deps.Clock,
			DummyHash: dummyHash,
		},
	)
	handlers := &handlers{service: service}
	router.HandleFunc(httpx.Operation{Method: http.MethodPost, Path: "/v1/auth/password/register", ID: "auth.password.register"}, handlers.register)
	router.HandleFunc(httpx.Operation{Method: http.MethodPost, Path: "/v1/auth/password/login", ID: "auth.password.login"}, handlers.login)
	router.HandleFunc(httpx.Operation{Method: http.MethodPost, Path: "/v1/auth/refresh", ID: "auth.refresh"}, handlers.refresh)
	router.HandleFunc(httpx.Operation{Method: http.MethodPost, Path: "/v1/auth/logout", ID: "auth.logout"}, handlers.logout)
	return nil
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
	httpx.Data(w, r, authResult(result))
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

func authResult(result app.Result) contracts.AuthResultWithMeDto {
	methods := make([]contracts.MeDtoAuthMethods, 0, len(result.AuthMethods))
	for _, method := range result.AuthMethods {
		methods = append(methods, contracts.MeDtoAuthMethods(method))
	}
	return contracts.AuthResultWithMeDto{
		User: contracts.MeDto{
			Id:            result.User.ID,
			Email:         result.User.Email,
			EmailVerified: result.User.EmailVerified,
			Roles:         []string{result.User.Role},
			AuthMethods:   methods,
			Profile:       contracts.MeProfileDto{},
		},
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
	}
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
