//go:build !pg

// Package infra implements the me feature's adapters.
// This build is SQLite until a pg repository exists.
package infra

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/fikrilal/go-core-kit/internal/db/gen"
	"github.com/fikrilal/go-core-kit/internal/features/me/domain"
	"github.com/fikrilal/go-core-kit/internal/features/me/ports"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
	"github.com/fikrilal/go-core-kit/internal/platform/db/utctime"
	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
)

// Repository is the sqlc-backed me repository.
type Repository struct {
	database *db.Database
	queries  *gen.Queries
}

// NewRepository builds the repository.
func NewRepository(database *db.Database) *Repository {
	return &Repository{database: database, queries: gen.New(database.SQL())}
}

// FindUserStatus returns the account status, reporting whether the user exists.
func (r *Repository) FindUserStatus(ctx context.Context, userID string) (string, bool, error) {
	status, err := r.queries.GetUserStatus(ctx, userID)
	if errors.Is(err, db.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find user status: %w", err)
	}
	return status, true, nil
}

// RevokeSession revokes one of the user's sessions and its refresh tokens.
// An already-revoked session still reports found.
func (r *Repository) RevokeSession(ctx context.Context, userID, sessionID string, now time.Time) (bool, error) {
	nowStamp := utctime.Time{Time: now}
	found := false
	err := r.database.InTx(ctx, func(tx db.Tx) error {
		queries := gen.New(tx)
		session, err := queries.GetUserSession(ctx, sessionID)
		if errors.Is(err, db.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("find session: %w", err)
		}
		if session.UserID != userID {
			return nil
		}
		found = true
		if session.RevokedAt.IsZero() {
			if _, err := queries.RevokeSession(ctx, gen.RevokeSessionParams{RevokedAt: nowStamp, ID: sessionID}); err != nil {
				return fmt.Errorf("revoke session: %w", err)
			}
		}
		if err := queries.ClearSessionDeviceState(ctx, gen.ClearSessionDeviceStateParams{Now: nowStamp, ID: sessionID}); err != nil {
			return fmt.Errorf("clear session device state: %w", err)
		}
		if _, err := queries.RevokeSessionRefreshTokens(ctx, gen.RevokeSessionRefreshTokensParams{RevokedAt: nowStamp, SessionID: sessionID}); err != nil {
			return fmt.Errorf("revoke session refresh tokens: %w", err)
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("revoke session: %w", err)
	}
	return found, nil
}

// UpsertPushToken attaches a push token to the caller's live session. The
// caller's session is validated first, so a dead session cannot detach a token
// from another session (reference comment).
func (r *Repository) UpsertPushToken(ctx context.Context, in ports.UpsertPushTokenInput) (bool, error) {
	now := utctime.Time{Time: in.Now}
	found := false
	err := r.database.InTx(ctx, func(tx db.Tx) error {
		queries := gen.New(tx)
		touched, err := queries.TouchSessionPushToken(ctx, gen.TouchSessionPushTokenParams{
			Now:    now,
			ID:     in.SessionID,
			UserID: in.UserID,
		})
		if err != nil {
			return fmt.Errorf("touch session: %w", err)
		}
		if touched != 1 {
			return nil
		}
		found = true
		if _, err := queries.ClearOtherSessionPushToken(ctx, gen.ClearOtherSessionPushTokenParams{
			Now:       now,
			PushToken: in.Token,
			ID:        in.SessionID,
		}); err != nil {
			return fmt.Errorf("clear other session token: %w", err)
		}
		if err := queries.SetSessionPushToken(ctx, gen.SetSessionPushTokenParams{
			PushPlatform: in.Platform,
			PushToken:    in.Token,
			Now:          now,
			ID:           in.SessionID,
		}); err != nil {
			return fmt.Errorf("set session token: %w", err)
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("upsert push token: %w", err)
	}
	return found, nil
}

// RevokePushToken clears the caller session's push token. There is no
// session-state gate: sign-out revocation is best-effort.
func (r *Repository) RevokePushToken(ctx context.Context, userID, sessionID string, now time.Time) error {
	nowStamp := utctime.Time{Time: now}
	if _, err := r.queries.ClearSessionPushToken(ctx, gen.ClearSessionPushTokenParams{
		Now:    nowStamp,
		ID:     sessionID,
		UserID: userID,
	}); err != nil {
		return fmt.Errorf("clear session push token: %w", err)
	}
	return nil
}

// CreateProfileImageFile records an UPLOADING file; a missing or deleted
// owner reports found false.
func (r *Repository) CreateProfileImageFile(ctx context.Context, in ports.CreateStoredFileInput) (bool, error) {
	now := utctime.Time{Time: in.Now}
	found := false
	err := r.database.InTx(ctx, func(tx db.Tx) error {
		queries := gen.New(tx)
		status, err := queries.GetUserStatus(ctx, in.OwnerUserID)
		if errors.Is(err, db.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("find user: %w", err)
		}
		if status == domain.StatusDeleted {
			return nil
		}
		found = true
		return queries.CreateStoredFile(ctx, gen.CreateStoredFileParams{
			ID:          in.ID,
			OwnerUserID: in.OwnerUserID,
			Bucket:      in.Bucket,
			ObjectKey:   in.ObjectKey,
			ContentType: in.ContentType,
			SizeBytes:   in.SizeBytes,
			TraceID:     in.TraceID,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	})
	if err != nil {
		return false, fmt.Errorf("create profile image file: %w", err)
	}
	return found, nil
}

// FindStoredFile loads one stored file owned by the caller.
func (r *Repository) FindStoredFile(ctx context.Context, fileID, userID string) (domain.StoredFile, bool, error) {
	row, err := r.queries.GetStoredFileForOwner(ctx, gen.GetStoredFileForOwnerParams{ID: fileID, OwnerUserID: userID})
	if errors.Is(err, db.ErrNoRows) {
		return domain.StoredFile{}, false, nil
	}
	if err != nil {
		return domain.StoredFile{}, false, fmt.Errorf("find stored file: %w", err)
	}
	return domain.StoredFile{
		ID:          row.ID,
		Status:      row.Status,
		ObjectKey:   row.ObjectKey,
		ContentType: row.ContentType,
		SizeBytes:   row.SizeBytes,
	}, true, nil
}

// MarkStoredFileDeleted marks one stored file deleted.
func (r *Repository) MarkStoredFileDeleted(ctx context.Context, fileID, userID string, now time.Time) error {
	stamp := utctime.Time{Time: now}
	if _, err := r.queries.MarkStoredFileDeleted(ctx, gen.MarkStoredFileDeletedParams{
		Now:         stamp,
		ID:          fileID,
		OwnerUserID: userID,
	}); err != nil {
		return fmt.Errorf("mark stored file deleted: %w", err)
	}
	return nil
}

// ProfileImage reads the caller's attached image; found false is a missing or
// deleted user.
func (r *Repository) ProfileImage(ctx context.Context, userID string) (ports.ProfileImageState, bool, error) {
	status, err := r.queries.GetUserStatus(ctx, userID)
	if errors.Is(err, db.ErrNoRows) {
		return ports.ProfileImageState{}, false, nil
	}
	if err != nil {
		return ports.ProfileImageState{}, false, fmt.Errorf("find user: %w", err)
	}
	if status == domain.StatusDeleted {
		return ports.ProfileImageState{}, false, nil
	}
	fileID, err := r.queries.GetUserProfileImageFileID(ctx, userID)
	if errors.Is(err, db.ErrNoRows) || !fileID.Valid {
		return ports.ProfileImageState{}, true, nil
	}
	if err != nil {
		return ports.ProfileImageState{}, false, fmt.Errorf("find profile image: %w", err)
	}
	state := ports.ProfileImageState{FileID: fileID.String}
	current, err := r.queries.GetCurrentProfileImageFile(ctx, userID)
	if err != nil && !errors.Is(err, db.ErrNoRows) {
		return ports.ProfileImageState{}, false, fmt.Errorf("find profile image file: %w", err)
	}
	state.ObjectKey = current.ObjectKey
	return state, true, nil
}

// requireLiveUser reports whether the user exists and is not deleted.
func requireLiveUser(ctx context.Context, queries *gen.Queries, userID string) (bool, error) {
	status, err := queries.GetUserStatus(ctx, userID)
	if errors.Is(err, db.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("find user: %w", err)
	}
	return status != domain.StatusDeleted, nil
}

// previousImage resolves the attached file and its object key, unless it is
// the file being attached again.
func previousImage(ctx context.Context, queries *gen.Queries, userID, keepFileID string) (string, string, error) {
	fileID, err := queries.GetUserProfileImageFileID(ctx, userID)
	if err != nil && !errors.Is(err, db.ErrNoRows) {
		return "", "", fmt.Errorf("find previous image: %w", err)
	}
	if !fileID.Valid || fileID.String == keepFileID {
		return "", "", nil
	}
	current, err := queries.GetCurrentProfileImageFile(ctx, userID)
	if err != nil && !errors.Is(err, db.ErrNoRows) {
		return "", "", fmt.Errorf("find previous image file: %w", err)
	}
	return fileID.String, current.ObjectKey, nil
}

// AttachProfileImage marks the file active, points the profile at it, and
// marks any replaced file deleted, all in one transaction. It returns the
// replaced object key for best-effort cleanup.
func (r *Repository) AttachProfileImage(ctx context.Context, in ports.AttachProfileImageInput) (string, bool, error) {
	now := utctime.Time{Time: in.Now}
	previousObjectKey := ""
	found := false
	err := r.database.InTx(ctx, func(tx db.Tx) error {
		queries := gen.New(tx)
		live, err := requireLiveUser(ctx, queries, in.UserID)
		if err != nil {
			return err
		}
		if !live {
			return nil
		}
		found = true
		previousFileID, objectKey, err := previousImage(ctx, queries, in.UserID, in.FileID)
		if err != nil {
			return err
		}
		previousObjectKey = objectKey
		if _, err := queries.MarkStoredFileActive(ctx, gen.MarkStoredFileActiveParams{
			Now:         now,
			ID:          in.FileID,
			OwnerUserID: in.UserID,
		}); err != nil {
			return fmt.Errorf("mark stored file active: %w", err)
		}
		if err := queries.SetUserProfileImageFileID(ctx, gen.SetUserProfileImageFileIDParams{
			UserID: in.UserID,
			FileID: in.FileID,
			Now:    now,
		}); err != nil {
			return fmt.Errorf("set profile image: %w", err)
		}
		if previousFileID != "" {
			if _, err := queries.MarkStoredFileDeleted(ctx, gen.MarkStoredFileDeletedParams{
				Now:         now,
				ID:          previousFileID,
				OwnerUserID: in.UserID,
			}); err != nil {
				return fmt.Errorf("mark previous file deleted: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return "", false, fmt.Errorf("attach profile image: %w", err)
	}
	return previousObjectKey, found, nil
}

// ClearProfileImage detaches the caller's image, marks the file deleted, and
// returns the cleared object key for best-effort cleanup.
func (r *Repository) ClearProfileImage(ctx context.Context, userID string, now time.Time) (string, bool, error) {
	stamp := utctime.Time{Time: now}
	clearedObjectKey := ""
	found := false
	err := r.database.InTx(ctx, func(tx db.Tx) error {
		queries := gen.New(tx)
		status, err := queries.GetUserStatus(ctx, userID)
		if errors.Is(err, db.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("find user: %w", err)
		}
		if status == domain.StatusDeleted {
			return nil
		}
		found = true
		fileID, err := queries.GetUserProfileImageFileID(ctx, userID)
		if err != nil && !errors.Is(err, db.ErrNoRows) {
			return fmt.Errorf("find profile image: %w", err)
		}
		if !fileID.Valid {
			return nil
		}
		current, err := queries.GetCurrentProfileImageFile(ctx, userID)
		if err != nil && !errors.Is(err, db.ErrNoRows) {
			return fmt.Errorf("find profile image file: %w", err)
		}
		clearedObjectKey = current.ObjectKey
		if _, err := queries.ClearUserProfileImageFileID(ctx, gen.ClearUserProfileImageFileIDParams{
			Now:    stamp,
			UserID: userID,
		}); err != nil {
			return fmt.Errorf("clear profile image: %w", err)
		}
		if _, err := queries.MarkStoredFileDeleted(ctx, gen.MarkStoredFileDeletedParams{
			Now:         stamp,
			ID:          fileID.String,
			OwnerUserID: userID,
		}); err != nil {
			return fmt.Errorf("mark stored file deleted: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", false, fmt.Errorf("clear profile image: %w", err)
	}
	return clearedObjectKey, found, nil
}

// RequestAccountDeletion schedules the deletion and records the audit row.
// A repeated request and the last-admin invariant both leave the row alone.
func (r *Repository) RequestAccountDeletion(ctx context.Context, in ports.RequestAccountDeletionInput) (domain.DeletionKind, error) {
	now := utctime.Time{Time: in.Now}
	kind := domain.DeletionOK
	err := r.database.InTx(ctx, func(tx db.Tx) error {
		queries := gen.New(tx)
		state, err := queries.GetUserDeletionState(ctx, in.UserID)
		if errors.Is(err, db.ErrNoRows) {
			kind = domain.DeletionNotFound
			return nil
		}
		if err != nil {
			return fmt.Errorf("find user: %w", err)
		}
		if state.Status == domain.StatusDeleted {
			kind = domain.DeletionNotFound
			return nil
		}
		if state.Role == domain.RoleAdmin && state.Status == domain.StatusActive && state.DeletionScheduledFor.IsZero() {
			admins, err := queries.CountActiveAdmins(ctx)
			if err != nil {
				return fmt.Errorf("count active admins: %w", err)
			}
			if admins <= 1 {
				kind = domain.DeletionLastAdmin
				return nil
			}
		}
		if !state.DeletionScheduledFor.IsZero() {
			kind = domain.DeletionAlreadyRequested
			return nil
		}
		if _, err := queries.SetUserDeletionRequest(ctx, gen.SetUserDeletionRequestParams{
			RequestedAt:  now,
			ScheduledFor: utctime.Time{Time: in.ScheduledFor},
			SessionID:    in.SessionID,
			TraceID:      in.TraceID,
			UpdatedAt:    now,
			ID:           in.UserID,
		}); err != nil {
			return fmt.Errorf("set deletion request: %w", err)
		}
		return recordDeletionAudit(ctx, queries, in.UserID, in.SessionID, in.TraceID, "REQUESTED", now)
	})
	if err != nil {
		return "", fmt.Errorf("request account deletion: %w", err)
	}
	return kind, nil
}

// CancelAccountDeletion clears a scheduled deletion and records the audit row.
func (r *Repository) CancelAccountDeletion(ctx context.Context, in ports.CancelAccountDeletionInput) (domain.DeletionKind, error) {
	now := utctime.Time{Time: in.Now}
	kind := domain.DeletionOK
	err := r.database.InTx(ctx, func(tx db.Tx) error {
		queries := gen.New(tx)
		state, err := queries.GetUserDeletionState(ctx, in.UserID)
		if errors.Is(err, db.ErrNoRows) {
			kind = domain.DeletionNotFound
			return nil
		}
		if err != nil {
			return fmt.Errorf("find user: %w", err)
		}
		if state.Status == domain.StatusDeleted {
			kind = domain.DeletionNotFound
			return nil
		}
		if state.DeletionScheduledFor.IsZero() {
			kind = domain.DeletionNotRequested
			return nil
		}
		if _, err := queries.ClearUserDeletionRequest(ctx, gen.ClearUserDeletionRequestParams{
			UpdatedAt: now,
			ID:        in.UserID,
		}); err != nil {
			return fmt.Errorf("clear deletion request: %w", err)
		}
		return recordDeletionAudit(ctx, queries, in.UserID, in.SessionID, in.TraceID, "CANCELED", now)
	})
	if err != nil {
		return "", fmt.Errorf("cancel account deletion: %w", err)
	}
	return kind, nil
}

// recordDeletionAudit appends one account-deletion audit row.
func recordDeletionAudit(ctx context.Context, queries *gen.Queries, userID, sessionID, traceID, action string, now utctime.Time) error {
	auditID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate audit id: %w", err)
	}
	if err := queries.CreateAccountDeletionAudit(ctx, gen.CreateAccountDeletionAuditParams{
		ID:             auditID.String(),
		ActorUserID:    userID,
		ActorSessionID: sessionID,
		TargetUserID:   userID,
		Action:         action,
		TraceID:        traceID,
		CreatedAt:      now,
	}); err != nil {
		return fmt.Errorf("create deletion audit: %w", err)
	}
	return nil
}

// sessionSortCreatedAt and sessionSortID are the cursor sort names.
const (
	sessionSortCreatedAt = "createdAt"
	sessionSortID        = "id"
)

// sessionColumns maps the allowed sort fields to their columns.
var sessionColumns = map[string]string{
	sessionSortCreatedAt: "created_at",
	sessionSortID:        "id",
}

// sessionPageSelect is the projection shared by every page. sqlc does not
// express the keyset variants, so this list SQL is hand-written here.
const sessionPageSelect = `SELECT id, device_id, device_name, ip, user_agent, last_seen_at, created_at, expires_at, revoked_at
FROM sessions
WHERE user_id = ?`

// sessionRow is one scanned session; nullable columns scan into pointers and
// a NULL timestamp scans as the zero time.
type sessionRow struct {
	ID         string
	DeviceID   *string
	DeviceName *string
	IP         *string
	UserAgent  *string
	LastSeenAt utctime.Time
	CreatedAt  utctime.Time
	ExpiresAt  utctime.Time
	RevokedAt  utctime.Time
}

func (r sessionRow) toDomain() domain.Session {
	return domain.Session{
		ID:         r.ID,
		DeviceID:   derefString(r.DeviceID),
		DeviceName: derefString(r.DeviceName),
		IP:         derefString(r.IP),
		UserAgent:  derefString(r.UserAgent),
		LastSeenAt: r.LastSeenAt.Time,
		CreatedAt:  r.CreatedAt.Time,
		ExpiresAt:  r.ExpiresAt.Time,
		RevokedAt:  r.RevokedAt.Time,
	}
}

// derefString flattens a scanned nullable column.
func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// ListSessions reads one keyset page: limit + 1 rows, then the extra row
// decides hasMore and the next cursor.
func (r *Repository) ListSessions(ctx context.Context, in ports.ListSessionsInput) (ports.SessionPage, error) {
	sort := effectiveSessionSort(in.Sort)
	queryText, args, err := sessionPageQuery(in, sort)
	if err != nil {
		return ports.SessionPage{}, err
	}
	rows, err := r.database.SQL().QueryContext(ctx, queryText, args...)
	if err != nil {
		return ports.SessionPage{}, fmt.Errorf("list sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	sessions := make([]domain.Session, 0, in.Limit+1)
	for rows.Next() {
		var row sessionRow
		if err := rows.Scan(&row.ID, &row.DeviceID, &row.DeviceName, &row.IP, &row.UserAgent,
			&row.LastSeenAt, &row.CreatedAt, &row.ExpiresAt, &row.RevokedAt); err != nil {
			return ports.SessionPage{}, fmt.Errorf("scan session: %w", err)
		}
		sessions = append(sessions, row.toDomain())
	}
	if err := rows.Err(); err != nil {
		return ports.SessionPage{}, fmt.Errorf("iterate sessions: %w", err)
	}

	page := ports.SessionPage{Sessions: sessions}
	if len(sessions) > in.Limit {
		page.Sessions = sessions[:in.Limit]
		page.HasMore = true
		page.NextCursor = encodeSessionCursor(sort, page.Sessions[len(page.Sessions)-1])
	}
	return page, nil
}

// effectiveSessionSort appends the id tie-breaker the reference config uses.
func effectiveSessionSort(fields []ports.SortField) []ports.SortField {
	if len(fields) == 0 {
		return []ports.SortField{{Field: sessionSortCreatedAt, Desc: true}, {Field: sessionSortID, Desc: true}}
	}
	sort := slices.Clone(fields)
	for _, field := range sort {
		if field.Field == sessionSortID {
			return sort
		}
	}
	return append(sort, ports.SortField{Field: sessionSortID, Desc: true})
}

// sessionPageQuery builds the keyset query for the effective sort.
func sessionPageQuery(in ports.ListSessionsInput, sort []ports.SortField) (string, []any, error) {
	args := []any{in.UserID}
	var cursor httpx.Cursor
	if in.Cursor != "" {
		decoded, err := httpx.DecodeCursor(in.Cursor, sessionSortKey(sort), sessionSortNames(sort))
		if err != nil {
			return "", nil, fmt.Errorf("decode cursor: %w", err)
		}
		cursor = decoded
	}
	where, cursorArgs, err := sessionKeyset(sort, cursor)
	if err != nil {
		return "", nil, err
	}
	args = append(args, cursorArgs...)
	args = append(args, in.Limit+1)
	return sessionPageSelect + where + " ORDER BY " + sessionOrderBy(sort) + " LIMIT ?", args, nil
}

// sessionKeyset builds the keyset predicate: for each sort field, a lexicographic
// comparison expanded as (f1 cmp v1) OR (f1 = v1 AND f2 cmp v2) OR ...
func sessionKeyset(sort []ports.SortField, cursor httpx.Cursor) (string, []any, error) {
	if cursor.After == nil {
		return "", nil, nil
	}
	clauses := make([]string, 0, len(sort))
	args := make([]any, 0, len(sort)*(len(sort)+1)/2)
	for i, field := range sort {
		parts := make([]string, 0, i+1)
		for j := 0; j <= i; j++ {
			column, err := sessionColumn(sort[j].Field)
			if err != nil {
				return "", nil, err
			}
			value, ok := cursor.After[sort[j].Field]
			if !ok {
				return "", nil, fmt.Errorf("cursor misses sort field %q", sort[j].Field)
			}
			if j < i {
				parts = append(parts, column+" = ?")
			} else {
				comparison := ">"
				if field.Desc {
					comparison = "<"
				}
				parts = append(parts, column+" "+comparison+" ?")
			}
			args = append(args, value)
		}
		clauses = append(clauses, "("+strings.Join(parts, " AND ")+")")
	}
	return " AND (" + strings.Join(clauses, " OR ") + ")", args, nil
}

// sessionOrderBy renders the ORDER BY for the effective sort.
func sessionOrderBy(sort []ports.SortField) string {
	parts := make([]string, 0, len(sort))
	for _, field := range sort {
		direction := "ASC"
		if field.Desc {
			direction = "DESC"
		}
		parts = append(parts, sessionColumns[field.Field]+" "+direction)
	}
	return strings.Join(parts, ", ")
}

// sessionSortKey renders the effective sort as the cursor's normalized form.
func sessionSortKey(sort []ports.SortField) string {
	parts := make([]string, 0, len(sort))
	for _, field := range sort {
		prefix := ""
		if field.Desc {
			prefix = "-"
		}
		parts = append(parts, prefix+field.Field)
	}
	return strings.Join(parts, ",")
}

// sessionSortNames lists the effective sort fields for cursor validation.
func sessionSortNames(sort []ports.SortField) []string {
	names := make([]string, 0, len(sort))
	for _, field := range sort {
		names = append(names, field.Field)
	}
	return names
}

// sessionColumn resolves an allowed sort field to its column.
func sessionColumn(field string) (string, error) {
	column, ok := sessionColumns[field]
	if !ok {
		return "", fmt.Errorf("unknown session sort field %q", field)
	}
	return column, nil
}

// encodeSessionCursor stores the kept row's sort values for the next page.
func encodeSessionCursor(sort []ports.SortField, last domain.Session) string {
	after := make(map[string]string, len(sort))
	for _, field := range sort {
		switch field.Field {
		case sessionSortCreatedAt:
			after[field.Field] = utctime.Time{Time: last.CreatedAt}.Stored()
		case sessionSortID:
			after[field.Field] = last.ID
		}
	}
	return httpx.EncodeCursor(sessionSortKey(sort), after)
}

// GetMe composes the user, profile, and auth methods.
func (r *Repository) GetMe(ctx context.Context, userID string) (domain.MeView, bool, error) {
	return meView(ctx, r.queries, userID)
}

// UpdateProfile touches the user, applies the patch to the stored profile, and
// re-reads the view, all in one transaction.
func (r *Repository) UpdateProfile(ctx context.Context, in ports.UpdateProfileInput) (domain.MeView, bool, error) {
	now := utctime.Time{Time: in.Now}
	var view domain.MeView
	found := false
	err := r.database.InTx(ctx, func(tx db.Tx) error {
		queries := gen.New(tx)
		touched, err := queries.TouchUser(ctx, gen.TouchUserParams{UpdatedAt: now, ID: in.UserID})
		if err != nil {
			return fmt.Errorf("touch user: %w", err)
		}
		if touched != 1 {
			return nil
		}
		if err := mergeProfilePatch(ctx, queries, in.UserID, in.Profile, now); err != nil {
			return err
		}
		view, found, err = meView(ctx, queries, in.UserID)
		return err
	})
	if err != nil {
		return domain.MeView{}, false, fmt.Errorf("update profile: %w", err)
	}
	return view, found, nil
}

// mergeProfilePatch applies the patch over the stored profile and upserts it.
func mergeProfilePatch(ctx context.Context, queries *gen.Queries, userID string, patch domain.ProfilePatch, now utctime.Time) error {
	existing, err := queries.GetUserProfile(ctx, userID)
	if errors.Is(err, db.ErrNoRows) {
		existing = gen.GetUserProfileRow{}
	} else if err != nil {
		return fmt.Errorf("find user profile: %w", err)
	}
	if err := queries.UpsertUserProfile(ctx, gen.UpsertUserProfileParams{
		UserID:      userID,
		DisplayName: mergeField(patch.DisplayName, existing.DisplayName.Valid, existing.DisplayName.String),
		GivenName:   mergeField(patch.GivenName, existing.GivenName.Valid, existing.GivenName.String),
		FamilyName:  mergeField(patch.FamilyName, existing.FamilyName.Valid, existing.FamilyName.String),
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		return fmt.Errorf("upsert user profile: %w", err)
	}
	return nil
}

// mergeField applies one patch field: absent keeps the stored value, and an
// explicit null clears it. The SQL NULLIF turns the cleared empty string into
// NULL.
func mergeField(patch domain.Field, storedValid bool, storedValue string) string {
	if !patch.Present {
		if !storedValid {
			return ""
		}
		return storedValue
	}
	if patch.Null {
		return ""
	}
	return patch.Value
}

// meView composes the view; a deleted user reads as not found.
func meView(ctx context.Context, queries *gen.Queries, userID string) (domain.MeView, bool, error) {
	row, err := queries.GetMeView(ctx, userID)
	if errors.Is(err, db.ErrNoRows) {
		return domain.MeView{}, false, nil
	}
	if err != nil {
		return domain.MeView{}, false, fmt.Errorf("get me view: %w", err)
	}
	if row.UserStatus == domain.StatusDeleted {
		return domain.MeView{}, false, nil
	}
	methods, err := queries.GetAuthMethods(ctx, userID)
	if err != nil {
		return domain.MeView{}, false, fmt.Errorf("get auth methods: %w", err)
	}
	return domain.MeView{
		User: domain.User{
			ID:            row.UserID,
			Email:         row.UserEmail,
			EmailVerified: !row.UserEmailVerifiedAt.IsZero(),
			Role:          row.UserRole,
			Status:        row.UserStatus,
		},
		Profile: domain.Profile{
			DisplayName: row.ProfileDisplayName.String,
			GivenName:   row.ProfileGivenName.String,
			FamilyName:  row.ProfileFamilyName.String,
		},
		AuthMethods:     orderAuthMethods(methods),
		AccountDeletion: accountDeletion(row.UserDeletionRequestedAt, row.UserDeletionScheduledFor),
	}, true, nil
}

// accountDeletion reports a scheduled deletion when both stamps are set.
func accountDeletion(requestedAt, scheduledFor utctime.Time) *domain.AccountDeletion {
	if requestedAt.IsZero() || scheduledFor.IsZero() {
		return nil
	}
	return &domain.AccountDeletion{RequestedAt: requestedAt.Time, ScheduledFor: scheduledFor.Time}
}

// orderAuthMethods sorts PASSWORD then GOOGLE. UNION does not order rows.
func orderAuthMethods(methods []string) []string {
	precedence := map[string]int{domain.AuthMethodPassword: 0, domain.AuthMethodGoogle: 1}
	ordered := make([]string, len(methods))
	copy(ordered, methods)
	slices.SortStableFunc(ordered, func(a, b string) int {
		return precedence[a] - precedence[b]
	})
	return ordered
}
