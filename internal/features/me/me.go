//go:build !pg

// Package me implements the me feature's HTTP surface and wiring.
// This build is SQLite until a pg repository exists.
package me

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/fikrilal/go-core-kit/contracts"
	"github.com/fikrilal/go-core-kit/internal/features/me/app"
	"github.com/fikrilal/go-core-kit/internal/features/me/domain"
	"github.com/fikrilal/go-core-kit/internal/features/me/infra"
	platformauth "github.com/fikrilal/go-core-kit/internal/platform/auth"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
	"github.com/fikrilal/go-core-kit/internal/platform/push"
	"github.com/fikrilal/go-core-kit/internal/platform/redis"
	"github.com/fikrilal/go-core-kit/internal/platform/storage"
)

// UploadLimitConfig is one upload limiter's attempts, window, and block; the
// feature root exposes it so the composition root never imports me/infra.
type UploadLimitConfig struct {
	MaxAttempts int
	Window      time.Duration
	Block       time.Duration
}

// Deps are the me feature's dependencies.
type Deps struct {
	Database *db.Database
	Redis    *redis.Client
	Clock    clock.Clock
	Guard    *platformauth.Guard
	Storage  *storage.Client
	// PushEnabled mirrors the reference's provider-configured gate until the
	// FCM adapter lands (5.3).
	PushEnabled     bool
	UploadUserLimit UploadLimitConfig
	UploadIPLimit   UploadLimitConfig
}

// Register mounts the me feature on the router.
func Register(router *httpx.Router, deps Deps) {
	service := app.New(
		app.Config{PushEnabled: deps.PushEnabled},
		app.Deps{
			Repository: infra.NewRepository(deps.Database),
			Storage:    deps.Storage,
			Clock:      deps.Clock,
		},
	)
	router.SetIdempotencyCaller(platformauth.IdempotencyCaller)
	handlers := &handlers{
		service: service,
		limiter: infra.NewUploadLimiter(deps.Redis, uploadLimit(deps.UploadUserLimit), uploadLimit(deps.UploadIPLimit)),
	}
	getMe := httpx.Operation{Method: http.MethodGet, Path: "/v1/me", ID: "users.me.get"}
	router.Handle(getMe, deps.Guard.Require(http.HandlerFunc(handlers.getMe)))
	patchMe := httpx.Operation{Method: http.MethodPatch, Path: "/v1/me", ID: "users.me.patch"}
	router.Handle(patchMe, deps.Guard.Require(router.Idempotent(patchMe, handlers.patchMe)))
	listSessions := httpx.Operation{Method: http.MethodGet, Path: "/v1/me/sessions", ID: "users.me.sessions.list"}
	router.Handle(listSessions, deps.Guard.Require(http.HandlerFunc(handlers.listSessions)))
	revokeSession := httpx.Operation{Method: http.MethodPost, Path: "/v1/me/sessions/{sessionId}/revoke", ID: "users.me.sessions.revoke"}
	router.Handle(revokeSession, deps.Guard.Require(http.HandlerFunc(handlers.revokeSession)))
	upsertPushToken := httpx.Operation{Method: http.MethodPut, Path: "/v1/me/push-token", ID: "users.me.pushToken.upsert"}
	router.Handle(upsertPushToken, deps.Guard.Require(http.HandlerFunc(handlers.upsertPushToken)))
	revokePushToken := httpx.Operation{Method: http.MethodDelete, Path: "/v1/me/push-token", ID: "users.me.pushToken.revoke"}
	router.Handle(revokePushToken, deps.Guard.Require(http.HandlerFunc(handlers.revokePushToken)))
	uploadPlan := httpx.Operation{Method: http.MethodPost, Path: "/v1/me/profile-image/upload", ID: "users.me.profileImage.upload"}
	router.Handle(uploadPlan, deps.Guard.Require(router.IdempotentFor(uploadPlan, app.ProfileImagePresignTTL, handlers.createUploadPlan)))
	completeUpload := httpx.Operation{Method: http.MethodPost, Path: "/v1/me/profile-image/complete", ID: "users.me.profileImage.complete"}
	router.Handle(completeUpload, deps.Guard.Require(http.HandlerFunc(handlers.completeUpload)))
	clearProfileImage := httpx.Operation{Method: http.MethodDelete, Path: "/v1/me/profile-image", ID: "users.me.profileImage.clear"}
	router.Handle(clearProfileImage, deps.Guard.Require(http.HandlerFunc(handlers.clearProfileImage)))
	profileImageURL := httpx.Operation{Method: http.MethodGet, Path: "/v1/me/profile-image/url", ID: "users.me.profileImage.url"}
	router.Handle(profileImageURL, deps.Guard.Require(http.HandlerFunc(handlers.profileImageURL)))
	requestDeletion := httpx.Operation{Method: http.MethodPost, Path: "/v1/me/account-deletion/request", ID: "users.me.accountDeletion.request"}
	router.Handle(requestDeletion, deps.Guard.Require(router.Idempotent(requestDeletion, handlers.requestAccountDeletion)))
	cancelDeletion := httpx.Operation{Method: http.MethodPost, Path: "/v1/me/account-deletion/cancel", ID: "users.me.accountDeletion.cancel"}
	router.Handle(cancelDeletion, deps.Guard.Require(router.Idempotent(cancelDeletion, handlers.cancelAccountDeletion)))
}

// PushJobDeps are the push handler dependencies.
type PushJobDeps struct {
	Database *db.Database
	Push     push.Sender
	Clock    clock.Clock
}

// NewPushJobs builds the push.send handler for the worker.
func NewPushJobs(deps PushJobDeps) *app.PushJobs {
	return app.NewPushJobs(app.PushJobDeps{
		Repository: infra.NewRepository(deps.Database),
		Push:       deps.Push,
		Clock:      deps.Clock,
	})
}

// uploadLimit converts the feature-level config into the limiter's shape.
func uploadLimit(config UploadLimitConfig) infra.UploadLimitConfig {
	return infra.UploadLimitConfig{
		MaxAttempts: config.MaxAttempts,
		Window:      config.Window,
		Block:       config.Block,
	}
}

// sessionsListConfig is the list contract for the sessions endpoint.
var sessionsListConfig = httpx.ListQueryConfig{
	DefaultLimit: 25,
	MaxLimit:     100,
	Sort: httpx.SortConfig{
		Allowed: []string{"createdAt", "id"},
		Default: []httpx.SortField{{Field: "createdAt", Desc: true}},
	},
}

type handlers struct {
	service *app.Service
	limiter *infra.UploadLimiter
}

func (h *handlers) getMe(w http.ResponseWriter, r *http.Request) {
	principal, ok := platformauth.PrincipalFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, &httpx.ProblemError{Status: http.StatusUnauthorized, Code: httpx.CodeUnauthorized})
		return
	}
	view, err := h.service.GetMe(r.Context(), principal.UserID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Data(w, r, meDto(view))
}

type patchMeRequest struct {
	Profile *patchMeProfile `json:"profile" validate:"required"`
}

type patchMeProfile struct {
	DisplayName httpx.OptionalString `json:"displayName"`
	GivenName   httpx.OptionalString `json:"givenName"`
	FamilyName  httpx.OptionalString `json:"familyName"`
}

func (h *handlers) patchMe(w http.ResponseWriter, r *http.Request) {
	principal, ok := platformauth.PrincipalFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, &httpx.ProblemError{Status: http.StatusUnauthorized, Code: httpx.CodeUnauthorized})
		return
	}
	var body patchMeRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	view, err := h.service.PatchMe(r.Context(), app.PatchMeInput{
		UserID: principal.UserID,
		Profile: domain.ProfilePatch{
			DisplayName: profileField(body.Profile.DisplayName),
			GivenName:   profileField(body.Profile.GivenName),
			FamilyName:  profileField(body.Profile.FamilyName),
		},
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Data(w, r, meDto(view))
}

// profileField converts a decoded wire field into the domain patch field.
func profileField(optional httpx.OptionalString) domain.Field {
	return domain.Field{Present: optional.Present(), Null: optional.Null(), Value: optional.Value()}
}

func (h *handlers) listSessions(w http.ResponseWriter, r *http.Request) {
	principal, ok := platformauth.PrincipalFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, &httpx.ProblemError{Status: http.StatusUnauthorized, Code: httpx.CodeUnauthorized})
		return
	}
	query, err := httpx.ParseListQuery(r, sessionsListConfig)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	page, err := h.service.ListSessions(r.Context(), app.ListSessionsInput{
		UserID:    principal.UserID,
		SessionID: principal.SessionID,
		Query:     query,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items := make([]contracts.MeSessionDto, 0, len(page.Views))
	for _, view := range page.Views {
		items = append(items, sessionDto(view))
	}
	httpx.List(w, r, items, httpx.ListMeta{Limit: query.Limit, HasMore: page.HasMore, NextCursor: page.NextCursor})
}

func (h *handlers) revokeSession(w http.ResponseWriter, r *http.Request) {
	principal, ok := platformauth.PrincipalFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, &httpx.ProblemError{Status: http.StatusUnauthorized, Code: httpx.CodeUnauthorized})
		return
	}
	sessionID := r.PathValue("sessionId")
	if _, err := uuid.Parse(sessionID); err != nil {
		httpx.WriteError(w, r, invalidSessionID())
		return
	}
	if err := h.service.RevokeSession(r.Context(), app.RevokeSessionInput{
		UserID:    principal.UserID,
		SessionID: sessionID,
	}); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.NoContent(w)
}

type pushTokenRequest struct {
	Platform string `json:"platform" validate:"required"`
	Token    string `json:"token" validate:"required"`
}

func (h *handlers) upsertPushToken(w http.ResponseWriter, r *http.Request) {
	principal, ok := platformauth.PrincipalFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, &httpx.ProblemError{Status: http.StatusUnauthorized, Code: httpx.CodeUnauthorized})
		return
	}
	var body pushTokenRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.service.UpsertPushToken(r.Context(), app.UpsertPushTokenInput{
		UserID:    principal.UserID,
		SessionID: principal.SessionID,
		Platform:  body.Platform,
		Token:     body.Token,
	}); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *handlers) revokePushToken(w http.ResponseWriter, r *http.Request) {
	principal, ok := platformauth.PrincipalFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, &httpx.ProblemError{Status: http.StatusUnauthorized, Code: httpx.CodeUnauthorized})
		return
	}
	if err := h.service.RevokePushToken(r.Context(), app.RevokePushTokenInput{
		UserID:    principal.UserID,
		SessionID: principal.SessionID,
	}); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.NoContent(w)
}

type profileImageUploadRequest struct {
	ContentType string `json:"contentType" validate:"required"`
	SizeBytes   int64  `json:"sizeBytes" validate:"required,gte=1,lte=5000000"`
}

func (h *handlers) createUploadPlan(w http.ResponseWriter, r *http.Request) {
	principal, ok := platformauth.PrincipalFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, &httpx.ProblemError{Status: http.StatusUnauthorized, Code: httpx.CodeUnauthorized})
		return
	}
	var body profileImageUploadRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.limiter.AssertAllowed(r.Context(), principal.UserID, clientIP(r)); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	plan, err := h.service.CreateUploadPlan(r.Context(), app.UploadPlanInput{
		UserID:      principal.UserID,
		ContentType: body.ContentType,
		SizeBytes:   body.SizeBytes,
		TraceID:     httpx.RequestID(r),
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Data(w, r, contracts.ProfileImageUploadPlanDto{
		FileId: plan.FileID,
		Upload: contracts.PresignedUploadDto{
			Method:  contracts.PresignedUploadDtoMethod(plan.Upload.Method),
			Url:     plan.Upload.URL,
			Headers: uploadHeaders(plan.Upload.Headers),
		},
		ExpiresAt: plan.ExpiresAt,
	})
}

type completeProfileImageRequest struct {
	FileID string `json:"fileId" validate:"required,uuid"`
}

func (h *handlers) completeUpload(w http.ResponseWriter, r *http.Request) {
	principal, ok := platformauth.PrincipalFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, &httpx.ProblemError{Status: http.StatusUnauthorized, Code: httpx.CodeUnauthorized})
		return
	}
	var body completeProfileImageRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.service.CompleteUpload(r.Context(), app.CompleteUploadInput{
		UserID: principal.UserID,
		FileID: body.FileID,
	}); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *handlers) clearProfileImage(w http.ResponseWriter, r *http.Request) {
	principal, ok := platformauth.PrincipalFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, &httpx.ProblemError{Status: http.StatusUnauthorized, Code: httpx.CodeUnauthorized})
		return
	}
	if err := h.service.ClearProfileImage(r.Context(), app.ClearProfileImageInput{UserID: principal.UserID}); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *handlers) profileImageURL(w http.ResponseWriter, r *http.Request) {
	principal, ok := platformauth.PrincipalFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, &httpx.ProblemError{Status: http.StatusUnauthorized, Code: httpx.CodeUnauthorized})
		return
	}
	url, hasImage, err := h.service.ProfileImageURL(r.Context(), principal.UserID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if !hasImage {
		httpx.NoContent(w)
		return
	}
	httpx.Data(w, r, contracts.ProfileImageUrlDto{Url: url.URL, ExpiresAt: url.ExpiresAt})
}

// accountDeletionCall runs a caller-scoped deletion action and writes 204.
func (h *handlers) accountDeletionCall(w http.ResponseWriter, r *http.Request, call func(ctx context.Context, userID, sessionID, traceID string) error) {
	principal, ok := platformauth.PrincipalFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, &httpx.ProblemError{Status: http.StatusUnauthorized, Code: httpx.CodeUnauthorized})
		return
	}
	if err := call(r.Context(), principal.UserID, principal.SessionID, httpx.RequestID(r)); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *handlers) requestAccountDeletion(w http.ResponseWriter, r *http.Request) {
	h.accountDeletionCall(w, r, func(ctx context.Context, userID, sessionID, traceID string) error {
		return h.service.RequestAccountDeletion(ctx, app.RequestAccountDeletionInput{
			UserID: userID, SessionID: sessionID, TraceID: traceID,
		})
	})
}

func (h *handlers) cancelAccountDeletion(w http.ResponseWriter, r *http.Request) {
	h.accountDeletionCall(w, r, func(ctx context.Context, userID, sessionID, traceID string) error {
		return h.service.CancelAccountDeletion(ctx, app.CancelAccountDeletionInput{
			UserID: userID, SessionID: sessionID, TraceID: traceID,
		})
	})
}

// uploadHeaders renders the presigned headers for the generated DTO.
func uploadHeaders(headers map[string]string) map[string]any {
	rendered := make(map[string]any, len(headers))
	for name, value := range headers {
		rendered[name] = value
	}
	return rendered
}

// clientIP is RemoteAddr only; forwarded headers are not trusted.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// invalidSessionID reports a path parameter that is not a UUID.
func invalidSessionID() error {
	return &httpx.ProblemError{
		Status: http.StatusBadRequest,
		Code:   httpx.CodeValidationFailed,
		Issues: []httpx.FieldIssue{{Field: "sessionId", Message: "must be a valid UUID"}},
	}
}

func sessionDto(view domain.SessionView) contracts.MeSessionDto {
	dto := contracts.MeSessionDto{
		Id:         view.ID,
		DeviceId:   nullableString(view.DeviceID),
		DeviceName: nullableString(view.DeviceName),
		Ip:         nullableString(view.IP),
		UserAgent:  nullableString(view.UserAgent),
		LastSeenAt: view.LastSeenAt,
		CreatedAt:  view.CreatedAt,
		ExpiresAt:  view.ExpiresAt,
		Current:    view.Current,
		Status:     contracts.MeSessionDtoStatus(view.Status),
	}
	if !view.RevokedAt.IsZero() {
		revokedAt := view.RevokedAt
		dto.RevokedAt = &revokedAt
	}
	return dto
}

func meDto(view domain.MeView) contracts.MeDto {
	methods := make([]contracts.MeDtoAuthMethods, 0, len(view.AuthMethods))
	for _, method := range view.AuthMethods {
		methods = append(methods, contracts.MeDtoAuthMethods(method))
	}
	return contracts.MeDto{
		Id:            view.User.ID,
		Email:         view.User.Email,
		EmailVerified: view.User.EmailVerified,
		Roles:         []string{view.User.Role},
		AuthMethods:   methods,
		Profile: contracts.MeProfileDto{
			DisplayName: nullableString(view.Profile.DisplayName),
			GivenName:   nullableString(view.Profile.GivenName),
			FamilyName:  nullableString(view.Profile.FamilyName),
		},
		AccountDeletion: accountDeletionDto(view.AccountDeletion),
	}
}

// accountDeletionDto renders the scheduled-deletion view.
func accountDeletionDto(deletion *domain.AccountDeletion) *contracts.AccountDeletionDto {
	if deletion == nil {
		return nil
	}
	return &contracts.AccountDeletionDto{
		RequestedAt:  deletion.RequestedAt,
		ScheduledFor: deletion.ScheduledFor,
	}
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
