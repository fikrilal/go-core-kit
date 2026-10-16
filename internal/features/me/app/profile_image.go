package app

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/fikrilal/go-core-kit/internal/features/me/domain"
	"github.com/fikrilal/go-core-kit/internal/features/me/ports"
	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
	"github.com/fikrilal/go-core-kit/internal/platform/storage"
)

// Profile image policy mirrors the reference.
const (
	profileImageMaxBytes = 5_000_000
	// ProfileImagePresignTTL is the upload plan lifetime; the idempotency
	// replay TTL uses it so a replay cannot return an expired URL.
	ProfileImagePresignTTL = 10 * time.Minute
	profileImageURLTTL     = 15 * time.Minute
)

// allowedProfileImageTypes are the accepted upload content types.
var allowedProfileImageTypes = []string{"image/jpeg", "image/png", "image/webp"}

// UploadPlan is the presigned upload the client executes.
type UploadPlan struct {
	FileID    string
	Upload    storage.PresignedPut
	ExpiresAt time.Time
}

// UploadPlanInput carries a profile image upload request.
type UploadPlanInput struct {
	UserID      string
	ContentType string
	SizeBytes   int64
	TraceID     string
}

// CreateUploadPlan creates the file record and presigns the client's PUT.
func (s *Service) CreateUploadPlan(ctx context.Context, in UploadPlanInput) (UploadPlan, error) {
	if !s.storage.Enabled() {
		return UploadPlan{}, objectStorageNotConfiguredError()
	}
	contentType := strings.TrimSpace(in.ContentType)
	if !slices.Contains(allowedProfileImageTypes, contentType) {
		return UploadPlan{}, validationFailedError([]httpx.FieldIssue{{
			Field:   "contentType",
			Message: "Allowed: " + strings.Join(allowedProfileImageTypes, ", "),
		}})
	}
	if in.SizeBytes <= 0 || in.SizeBytes > profileImageMaxBytes {
		return UploadPlan{}, validationFailedError([]httpx.FieldIssue{{
			Field:   "sizeBytes",
			Message: fmt.Sprintf("Must be between 1 and %d", profileImageMaxBytes),
		}})
	}
	now := s.clock.Now()
	fileID, err := uuid.NewV7()
	if err != nil {
		return UploadPlan{}, fmt.Errorf("me: generate file id: %w", err)
	}
	objectKey := fmt.Sprintf("users/%s/profile-images/%s", in.UserID, fileID)
	upload, err := s.storage.PresignPut(objectKey, contentType, ProfileImagePresignTTL)
	if err != nil {
		return UploadPlan{}, fmt.Errorf("me: presign upload: %w", err)
	}
	found, err := s.repo.CreateProfileImageFile(ctx, ports.CreateStoredFileInput{
		ID:          fileID.String(),
		OwnerUserID: in.UserID,
		Bucket:      s.storage.Bucket(),
		ObjectKey:   objectKey,
		ContentType: contentType,
		SizeBytes:   in.SizeBytes,
		TraceID:     in.TraceID,
		Now:         now,
	})
	if err != nil {
		return UploadPlan{}, fmt.Errorf("me: create upload record: %w", err)
	}
	if !found {
		return UploadPlan{}, unauthorizedError()
	}
	plan := UploadPlan{FileID: fileID.String(), Upload: upload, ExpiresAt: now.Add(ProfileImagePresignTTL)}
	// The expiry job is armed at plan time so an abandoned upload is cleaned up
	// even when the client never completes it; the delay is policy.
	if s.deletion != nil && s.uploadExpireDelay > 0 {
		s.deletion.ScheduleUploadExpiry(ctx, in.UserID, plan.FileID, now.Add(s.uploadExpireDelay), requestMeta(in.TraceID))
	}
	return plan, nil
}

// CompleteUploadInput carries an upload finalization.
type CompleteUploadInput struct {
	UserID string
	FileID string
}

// CompleteUpload verifies the uploaded object and attaches it to the profile.
// A size or type mismatch rejects the upload: object delete and mark deleted,
// both best-effort, mirroring the reference.
func (s *Service) CompleteUpload(ctx context.Context, in CompleteUploadInput) error {
	if !s.storage.Enabled() {
		return objectStorageNotConfiguredError()
	}
	file, found, err := s.repo.FindStoredFile(ctx, in.FileID, in.UserID)
	if err != nil {
		return fmt.Errorf("me: find stored file: %w", err)
	}
	if !found {
		return fileNotFoundError()
	}
	if file.Status == domain.FileStatusDeleted {
		return uploadNotUploadedError("Upload is not available")
	}
	state, userFound, err := s.repo.ProfileImage(ctx, in.UserID)
	if err != nil {
		return fmt.Errorf("me: read profile image: %w", err)
	}
	if !userFound {
		return unauthorizedError()
	}
	if file.Status == domain.FileStatusActive && state.FileID == in.FileID {
		return nil
	}
	head, err := s.storage.Head(ctx, file.ObjectKey)
	if err != nil {
		return fmt.Errorf("me: head upload: %w", err)
	}
	if !head.Exists {
		return uploadNotUploadedError("Upload not found")
	}
	if head.ContentLength == nil || *head.ContentLength != file.SizeBytes {
		s.rejectUpload(ctx, file, in.UserID)
		return sizeMismatchError()
	}
	if head.ContentType == nil || *head.ContentType != file.ContentType {
		s.rejectUpload(ctx, file, in.UserID)
		return contentTypeMismatchError()
	}
	previousObjectKey, found, err := s.repo.AttachProfileImage(ctx, ports.AttachProfileImageInput{
		UserID: in.UserID,
		FileID: in.FileID,
		Now:    s.clock.Now(),
	})
	if err != nil {
		return fmt.Errorf("me: attach profile image: %w", err)
	}
	if !found {
		return unauthorizedError()
	}
	if previousObjectKey != "" && previousObjectKey != file.ObjectKey {
		s.deleteObjectBestEffort(ctx, previousObjectKey)
	}
	return nil
}

// ClearProfileImageInput carries a profile image clear for the caller.
type ClearProfileImageInput struct {
	UserID string
}

// ClearProfileImage detaches the current image and marks the file deleted.
func (s *Service) ClearProfileImage(ctx context.Context, in ClearProfileImageInput) error {
	clearedObjectKey, found, err := s.repo.ClearProfileImage(ctx, in.UserID, s.clock.Now())
	if err != nil {
		return fmt.Errorf("me: clear profile image: %w", err)
	}
	if !found {
		return unauthorizedError()
	}
	if clearedObjectKey != "" {
		s.deleteObjectBestEffort(ctx, clearedObjectKey)
	}
	return nil
}

// ImageURL is the presigned read for the current profile image.
type ImageURL struct {
	URL       string
	ExpiresAt time.Time
}

// ProfileImageURL presigns a GET for the current image; hasImage false is the
// 204 case. A missing profile image answers before the storage gate, mirroring
// the reference.
func (s *Service) ProfileImageURL(ctx context.Context, userID string) (ImageURL, bool, error) {
	state, found, err := s.repo.ProfileImage(ctx, userID)
	if err != nil {
		return ImageURL{}, false, fmt.Errorf("me: read profile image: %w", err)
	}
	if !found {
		return ImageURL{}, false, unauthorizedError()
	}
	if state.FileID == "" {
		return ImageURL{}, false, nil
	}
	if !s.storage.Enabled() {
		return ImageURL{}, false, objectStorageNotConfiguredError()
	}
	presigned, err := s.storage.PresignGet(state.ObjectKey, profileImageURLTTL)
	if err != nil {
		return ImageURL{}, false, fmt.Errorf("me: presign read: %w", err)
	}
	return ImageURL{URL: presigned.URL, ExpiresAt: s.clock.Now().Add(profileImageURLTTL)}, true, nil
}

// rejectUpload best-effort deletes the object and marks the record deleted.
func (s *Service) rejectUpload(ctx context.Context, file domain.StoredFile, userID string) {
	s.deleteObjectBestEffort(ctx, file.ObjectKey)
	if err := s.repo.MarkStoredFileDeleted(ctx, file.ID, userID, s.clock.Now()); err != nil {
		// best-effort, like the reference; the durable cleanup job lands with the worker (5.x)
		_ = err
	}
}

// deleteObjectBestEffort removes an object without failing the request.
func (s *Service) deleteObjectBestEffort(ctx context.Context, objectKey string) {
	// best-effort: a failed delete is retried by the durable cleanup job (5.x)
	_ = s.storage.Delete(ctx, objectKey)
}
