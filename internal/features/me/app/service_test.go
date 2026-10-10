package app_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fikrilal/go-core-kit/internal/features/me/app"
	"github.com/fikrilal/go-core-kit/internal/features/me/domain"
	"github.com/fikrilal/go-core-kit/internal/features/me/ports"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
	"github.com/fikrilal/go-core-kit/internal/platform/storage"
)

var moment = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

type fakeRepository struct {
	view        domain.MeView
	viewFound   bool
	viewErr     error
	updateFound bool
	updateErr   error
	updates     []ports.UpdateProfileInput

	statusValue string
	statusFound bool
	statusErr   error

	listPage ports.SessionPage
	listErr  error
	lists    []ports.ListSessionsInput

	revokeOK    bool
	revokeErr   error
	revokeNow   time.Time
	revocations []string

	pushFound   bool
	pushErr     error
	pushNow     time.Time
	pushUpserts []ports.UpsertPushTokenInput
	pushRevokes []string

	createFileFound bool
	createFileErr   error
	createFiles     []ports.CreateStoredFileInput
	storedFile      domain.StoredFile
	storedFound     bool
	storedErr       error
	markedDeleted   []string
	markDeletedErr  error
	imageState      ports.ProfileImageState
	imageFound      bool
	imageErr        error
	attachPrev      string
	attachFound     bool
	attachErr       error
	attachInputs    []ports.AttachProfileImageInput
	clearKey        string
	clearFound      bool
	clearErr        error
}

func (f *fakeRepository) CreateProfileImageFile(_ context.Context, in ports.CreateStoredFileInput) (bool, error) {
	f.createFiles = append(f.createFiles, in)
	if f.createFileErr != nil {
		return false, f.createFileErr
	}
	return f.createFileFound, nil
}

func (f *fakeRepository) FindStoredFile(context.Context, string, string) (domain.StoredFile, bool, error) {
	return f.storedFile, f.storedFound, f.storedErr
}

func (f *fakeRepository) MarkStoredFileDeleted(_ context.Context, fileID, _ string, _ time.Time) error {
	f.markedDeleted = append(f.markedDeleted, fileID)
	return f.markDeletedErr
}

func (f *fakeRepository) ProfileImage(context.Context, string) (ports.ProfileImageState, bool, error) {
	return f.imageState, f.imageFound, f.imageErr
}

func (f *fakeRepository) AttachProfileImage(_ context.Context, in ports.AttachProfileImageInput) (string, bool, error) {
	f.attachInputs = append(f.attachInputs, in)
	if f.attachErr != nil {
		return "", false, f.attachErr
	}
	return f.attachPrev, f.attachFound, nil
}

func (f *fakeRepository) ClearProfileImage(context.Context, string, time.Time) (string, bool, error) {
	if f.clearErr != nil {
		return "", false, f.clearErr
	}
	return f.clearKey, f.clearFound, nil
}

type fakeStorage struct {
	enabled   bool
	heads     int
	head      storage.HeadResult
	headErr   error
	deleted   []string
	deleteErr error
	gets      []string
}

func (f *fakeStorage) Enabled() bool  { return f.enabled }
func (f *fakeStorage) Bucket() string { return "test-bucket" }

func (f *fakeStorage) PresignPut(key, contentType string, _ time.Duration) (storage.PresignedPut, error) {
	return storage.PresignedPut{
		Method:  http.MethodPut,
		URL:     "https://objects.test/" + key,
		Headers: map[string]string{"Content-Type": contentType},
	}, nil
}

func (f *fakeStorage) PresignGet(key string, _ time.Duration) (storage.PresignedGet, error) {
	f.gets = append(f.gets, key)
	return storage.PresignedGet{URL: "https://objects.test/" + key}, nil
}

func (f *fakeStorage) Head(context.Context, string) (storage.HeadResult, error) {
	f.heads++
	return f.head, f.headErr
}

func (f *fakeStorage) Delete(_ context.Context, key string) error {
	f.deleted = append(f.deleted, key)
	return f.deleteErr
}

func (f *fakeRepository) UpsertPushToken(_ context.Context, in ports.UpsertPushTokenInput) (bool, error) {
	f.pushUpserts = append(f.pushUpserts, in)
	f.pushNow = in.Now
	if f.pushErr != nil {
		return false, f.pushErr
	}
	return f.pushFound, nil
}

func (f *fakeRepository) RevokePushToken(_ context.Context, _, sessionID string, now time.Time) error {
	f.pushRevokes = append(f.pushRevokes, sessionID)
	f.pushNow = now
	return f.pushErr
}

func (f *fakeRepository) FindUserStatus(context.Context, string) (string, bool, error) {
	return f.statusValue, f.statusFound, f.statusErr
}

func (f *fakeRepository) ListSessions(_ context.Context, in ports.ListSessionsInput) (ports.SessionPage, error) {
	f.lists = append(f.lists, in)
	if f.listErr != nil {
		return ports.SessionPage{}, f.listErr
	}
	return f.listPage, nil
}

func (f *fakeRepository) RevokeSession(_ context.Context, _, sessionID string, now time.Time) (bool, error) {
	f.revocations = append(f.revocations, sessionID)
	f.revokeNow = now
	if f.revokeErr != nil {
		return false, f.revokeErr
	}
	return f.revokeOK, nil
}

func (f *fakeRepository) GetMe(context.Context, string) (domain.MeView, bool, error) {
	return f.view, f.viewFound, f.viewErr
}

func (f *fakeRepository) UpdateProfile(_ context.Context, in ports.UpdateProfileInput) (domain.MeView, bool, error) {
	f.updates = append(f.updates, in)
	if f.updateErr != nil {
		return domain.MeView{}, false, f.updateErr
	}
	return f.view, f.updateFound, nil
}

func newFixture() (*app.Service, *fakeRepository) {
	repo := &fakeRepository{updateFound: true}
	service := app.New(
		app.Config{PushEnabled: true},
		app.Deps{Repository: repo, Storage: &fakeStorage{enabled: true}, Clock: clock.Fixed{Moment: moment}},
	)
	return service, repo
}

func newImageFixture(enabled bool) (*app.Service, *fakeRepository, *fakeStorage) {
	repo := &fakeRepository{
		updateFound:     true,
		statusFound:     true,
		statusValue:     domain.StatusActive,
		createFileFound: true,
		imageFound:      true,
		attachFound:     true,
		clearFound:      true,
	}
	objectStorage := &fakeStorage{enabled: enabled}
	service := app.New(
		app.Config{},
		app.Deps{Repository: repo, Storage: objectStorage, Clock: clock.Fixed{Moment: moment}},
	)
	return service, repo, objectStorage
}

func problemError(t *testing.T, err error) *httpx.ProblemError {
	t.Helper()
	var problem *httpx.ProblemError
	if !errors.As(err, &problem) {
		t.Fatalf("error = %v, want ProblemError", err)
	}
	return problem
}

func TestGetMePassesThroughRepository(t *testing.T) {
	service, repo := newFixture()
	repo.view = domain.MeView{
		User:        domain.User{ID: "user-1", Email: "user@example.com", Role: domain.RoleUser},
		Profile:     domain.Profile{DisplayName: "Dante"},
		AuthMethods: []string{domain.AuthMethodPassword},
	}
	repo.viewFound = true
	view, err := service.GetMe(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("GetMe() error = %v", err)
	}
	if view.User.ID != "user-1" || view.Profile.DisplayName != "Dante" || len(view.AuthMethods) != 1 {
		t.Fatalf("view = %+v, want the repository view", view)
	}

	repo.viewFound = false
	problem := problemError(t, mustError(service.GetMe(context.Background(), "missing")))
	if problem.Status != http.StatusUnauthorized || problem.Code != httpx.CodeUnauthorized {
		t.Fatalf("missing user mapped to %d/%s, want 401 UNAUTHORIZED", problem.Status, problem.Code)
	}
}

func TestPatchMeAppliesFieldsAndClock(t *testing.T) {
	service, repo := newFixture()
	_, err := service.PatchMe(context.Background(), app.PatchMeInput{
		UserID: "user-1",
		Profile: domain.ProfilePatch{
			DisplayName: domain.Field{Present: true, Value: "Dante"},
			FamilyName:  domain.Field{Present: true, Null: true},
		},
	})
	if err != nil {
		t.Fatalf("PatchMe() error = %v", err)
	}
	if len(repo.updates) != 1 {
		t.Fatalf("updates = %d, want 1", len(repo.updates))
	}
	update := repo.updates[0]
	if !update.Now.Equal(moment) {
		t.Errorf("now = %v, want the injected clock", update.Now)
	}
	if update.Profile.DisplayName.Value != "Dante" || !update.Profile.DisplayName.Present {
		t.Errorf("displayName = %+v, want the set field", update.Profile.DisplayName)
	}
	if !update.Profile.FamilyName.Null || update.Profile.FamilyName.Value != "" {
		t.Errorf("familyName = %+v, want a cleared field", update.Profile.FamilyName)
	}
	if update.Profile.GivenName.Present {
		t.Errorf("givenName = %+v, want the absent field", update.Profile.GivenName)
	}
}

func TestPatchMeRequiresOneField(t *testing.T) {
	service, repo := newFixture()
	problem := problemError(t, mustError(service.PatchMe(context.Background(), app.PatchMeInput{UserID: "user-1"})))
	if problem.Status != http.StatusBadRequest || problem.Code != httpx.CodeValidationFailed {
		t.Fatalf("problem = %+v, want 400 VALIDATION_FAILED", problem)
	}
	if len(problem.Issues) != 1 || problem.Issues[0].Field != "profile" {
		t.Fatalf("issues = %+v, want the profile issue", problem.Issues)
	}
	const wantMessage = "At least one profile field must be provided"
	if problem.Issues[0].Message != wantMessage {
		t.Errorf("message = %q, want %q", problem.Issues[0].Message, wantMessage)
	}
	if len(repo.updates) != 0 {
		t.Error("repository called for an empty patch")
	}
}

func TestPatchMeValidatesFields(t *testing.T) {
	longName := strings.Repeat("a", 101)
	cases := []struct {
		name    string
		patch   domain.ProfilePatch
		field   string
		message string
	}{
		{
			name:    "empty value",
			patch:   domain.ProfilePatch{DisplayName: domain.Field{Present: true, Value: ""}},
			field:   "displayName",
			message: "must not be empty",
		},
		{
			name:    "over long value",
			patch:   domain.ProfilePatch{GivenName: domain.Field{Present: true, Value: longName}},
			field:   "givenName",
			message: "must be at most 100 characters",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, repo := newFixture()
			problem := problemError(t, mustError(service.PatchMe(context.Background(), app.PatchMeInput{UserID: "user-1", Profile: tc.patch})))
			if problem.Status != http.StatusBadRequest || problem.Code != httpx.CodeValidationFailed {
				t.Fatalf("problem = %+v, want 400 VALIDATION_FAILED", problem)
			}
			if len(problem.Issues) != 1 || problem.Issues[0].Field != tc.field || problem.Issues[0].Message != tc.message {
				t.Fatalf("issues = %+v, want %s %q", problem.Issues, tc.field, tc.message)
			}
			if len(repo.updates) != 0 {
				t.Error("repository called for an invalid patch")
			}
		})
	}

	t.Run("issues are collected in field order", func(t *testing.T) {
		service, _ := newFixture()
		problem := problemError(t, mustError(service.PatchMe(context.Background(), app.PatchMeInput{
			UserID: "user-1",
			Profile: domain.ProfilePatch{
				FamilyName:  domain.Field{Present: true, Value: longName},
				DisplayName: domain.Field{Present: true, Value: ""},
			},
		})))
		if len(problem.Issues) != 2 {
			t.Fatalf("issues = %+v, want two collected issues", problem.Issues)
		}
		if problem.Issues[0].Field != "displayName" || problem.Issues[1].Field != "familyName" {
			t.Errorf("issue order = %s, %s; want displayName first", problem.Issues[0].Field, problem.Issues[1].Field)
		}
	})
}

func TestPatchMeMissingUserIsUnauthorized(t *testing.T) {
	service, repo := newFixture()
	repo.updateFound = false
	problem := problemError(t, mustError(service.PatchMe(context.Background(), app.PatchMeInput{
		UserID:  "missing",
		Profile: domain.ProfilePatch{DisplayName: domain.Field{Present: true, Value: "Dante"}},
	})))
	if problem.Status != http.StatusUnauthorized || problem.Code != httpx.CodeUnauthorized {
		t.Fatalf("missing user mapped to %d/%s, want 401 UNAUTHORIZED", problem.Status, problem.Code)
	}
}

func mustError(_ domain.MeView, err error) error {
	return fmt.Errorf("call: %w", err)
}

func sessionsFixture() (*app.Service, *fakeRepository) {
	service, repo := newFixture()
	repo.statusFound = true
	repo.statusValue = domain.StatusActive
	return service, repo
}

func TestCreateUploadPlan(t *testing.T) {
	service, repo, _ := newImageFixture(true)
	plan, err := service.CreateUploadPlan(context.Background(), app.UploadPlanInput{
		UserID: "user-1", ContentType: " image/webp ", SizeBytes: 1234, TraceID: "trace-1",
	})
	if err != nil {
		t.Fatalf("CreateUploadPlan() error = %v", err)
	}
	if plan.Upload.Method != http.MethodPut || plan.Upload.Headers["Content-Type"] != "image/webp" {
		t.Errorf("upload = %+v, want a presigned PUT with the declared type", plan.Upload)
	}
	if !plan.ExpiresAt.Equal(moment.Add(10 * time.Minute)) {
		t.Errorf("expiresAt = %v, want now + 10 minutes", plan.ExpiresAt)
	}
	if len(repo.createFiles) != 1 {
		t.Fatalf("create inputs = %d, want 1", len(repo.createFiles))
	}
	created := repo.createFiles[0]
	wantKey := "users/user-1/profile-images/" + plan.FileID
	if created.ObjectKey != wantKey || created.Bucket != "test-bucket" || created.SizeBytes != 1234 {
		t.Errorf("created = %+v, want the plan's object key and metadata", created)
	}
	if created.TraceID != "trace-1" {
		t.Errorf("traceId = %q, want the request id", created.TraceID)
	}
}

func TestCreateUploadPlanGuards(t *testing.T) {
	disabled, _, _ := newImageFixture(false)
	problem := problemError(t, mustUploadPlanError(disabled.CreateUploadPlan(context.Background(), app.UploadPlanInput{
		UserID: "user-1", ContentType: "image/png", SizeBytes: 10,
	})))
	if problem.Status != http.StatusNotImplemented || problem.Code != httpx.CodeUsersObjectStorageNotConfigured {
		t.Fatalf("problem = %+v, want 501 USERS_OBJECT_STORAGE_NOT_CONFIGURED", problem)
	}

	cases := []struct {
		name        string
		contentType string
		sizeBytes   int64
		field       string
		message     string
	}{
		{name: "unknown type", contentType: "image/gif", sizeBytes: 10, field: "contentType", message: "Allowed: image/jpeg, image/png, image/webp"},
		{name: "zero size", contentType: "image/png", sizeBytes: 0, field: "sizeBytes", message: "Must be between 1 and 5000000"},
		{name: "over max", contentType: "image/png", sizeBytes: 5_000_001, field: "sizeBytes", message: "Must be between 1 and 5000000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, repo, _ := newImageFixture(true)
			problem := problemError(t, mustUploadPlanError(service.CreateUploadPlan(context.Background(), app.UploadPlanInput{
				UserID: "user-1", ContentType: tc.contentType, SizeBytes: tc.sizeBytes,
			})))
			if problem.Status != http.StatusBadRequest || problem.Code != httpx.CodeValidationFailed {
				t.Fatalf("problem = %+v, want 400 VALIDATION_FAILED", problem)
			}
			if len(problem.Issues) != 1 || problem.Issues[0].Field != tc.field || problem.Issues[0].Message != tc.message {
				t.Fatalf("issues = %+v, want %s %q", problem.Issues, tc.field, tc.message)
			}
			if len(repo.createFiles) != 0 {
				t.Error("record created for an invalid plan")
			}
		})
	}
}

func mustUploadPlanError(_ app.UploadPlan, err error) error {
	return fmt.Errorf("call: %w", err)
}

func mustSessionsError(_ app.SessionPage, err error) error {
	return fmt.Errorf("call: %w", err)
}

func uploadFixture(t *testing.T) (*app.Service, *fakeRepository, *fakeStorage, string) {
	t.Helper()
	service, repo, objectStorage := newImageFixture(true)
	repo.storedFound = true
	repo.storedFile = domain.StoredFile{
		ID: "file-1", Status: domain.FileStatusUploading, ObjectKey: "users/user-1/profile-images/file-1",
		ContentType: "image/webp", SizeBytes: 11,
	}
	length := int64(11)
	contentType := "image/webp"
	objectStorage.head = storage.HeadResult{Exists: true, ContentType: &contentType, ContentLength: &length}
	return service, repo, objectStorage, "file-1"
}

func TestCompleteUploadAttachesFile(t *testing.T) {
	service, repo, objectStorage, fileID := uploadFixture(t)
	if err := service.CompleteUpload(context.Background(), app.CompleteUploadInput{UserID: "user-1", FileID: fileID}); err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}
	if len(repo.attachInputs) != 1 || repo.attachInputs[0].FileID != fileID {
		t.Fatalf("attach inputs = %+v, want the file attach", repo.attachInputs)
	}
	if len(objectStorage.deleted) != 0 {
		t.Errorf("deleted = %v, want no cleanup for a fresh attach", objectStorage.deleted)
	}
}

func TestCompleteUploadIsIdempotentWhenAttached(t *testing.T) {
	service, repo, objectStorage, fileID := uploadFixture(t)
	repo.storedFile.Status = domain.FileStatusActive
	repo.imageState = ports.ProfileImageState{FileID: fileID}
	if err := service.CompleteUpload(context.Background(), app.CompleteUploadInput{UserID: "user-1", FileID: fileID}); err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}
	if objectStorage.heads != 0 || len(repo.attachInputs) != 0 {
		t.Error("an already-attached file was re-verified or re-attached")
	}
}

func TestCompleteUploadRecordRejections(t *testing.T) {
	t.Run("unknown file", func(t *testing.T) {
		service, repo, _, _ := uploadFixture(t)
		repo.storedFound = false
		problem := problemError(t, service.CompleteUpload(context.Background(), app.CompleteUploadInput{UserID: "user-1", FileID: "file-1"}))
		if problem.Status != http.StatusNotFound || problem.Code != httpx.CodeNotFound {
			t.Fatalf("problem = %+v, want 404 NOT_FOUND", problem)
		}
	})

	t.Run("deleted record", func(t *testing.T) {
		service, repo, _, _ := uploadFixture(t)
		repo.storedFile.Status = domain.FileStatusDeleted
		problem := problemError(t, service.CompleteUpload(context.Background(), app.CompleteUploadInput{UserID: "user-1", FileID: "file-1"}))
		if problem.Status != http.StatusConflict || problem.Code != httpx.CodeUsersProfileImageNotUploaded {
			t.Fatalf("problem = %+v, want 409 USERS_PROFILE_IMAGE_NOT_UPLOADED", problem)
		}
	})

	t.Run("missing object", func(t *testing.T) {
		service, repo, objectStorage, _ := uploadFixture(t)
		objectStorage.head = storage.HeadResult{}
		problem := problemError(t, service.CompleteUpload(context.Background(), app.CompleteUploadInput{UserID: "user-1", FileID: "file-1"}))
		if problem.Status != http.StatusConflict || problem.Code != httpx.CodeUsersProfileImageNotUploaded {
			t.Fatalf("problem = %+v, want 409 USERS_PROFILE_IMAGE_NOT_UPLOADED", problem)
		}
		if len(repo.markedDeleted) != 0 || len(objectStorage.deleted) != 0 {
			t.Error("a missing object was rejected with cleanup; the record stays UPLOADING")
		}
	})
}

func TestCompleteUploadMismatchRejections(t *testing.T) {
	t.Run("size mismatch", func(t *testing.T) {
		service, repo, objectStorage, _ := uploadFixture(t)
		length := int64(3)
		objectStorage.head.ContentLength = &length
		problem := problemError(t, service.CompleteUpload(context.Background(), app.CompleteUploadInput{UserID: "user-1", FileID: "file-1"}))
		if problem.Status != http.StatusConflict || problem.Code != httpx.CodeUsersProfileImageSizeMismatch {
			t.Fatalf("problem = %+v, want 409 USERS_PROFILE_IMAGE_SIZE_MISMATCH", problem)
		}
		if len(repo.markedDeleted) != 1 || len(objectStorage.deleted) != 1 {
			t.Errorf("cleanup = marked %v deleted %v, want the rejected upload removed", repo.markedDeleted, objectStorage.deleted)
		}
	})

	t.Run("content type mismatch", func(t *testing.T) {
		service, repo, objectStorage, _ := uploadFixture(t)
		mismatched := "image/png"
		objectStorage.head.ContentType = &mismatched
		problem := problemError(t, service.CompleteUpload(context.Background(), app.CompleteUploadInput{UserID: "user-1", FileID: "file-1"}))
		if problem.Status != http.StatusConflict || problem.Code != httpx.CodeUsersProfileImageContentTypeMismatch {
			t.Fatalf("problem = %+v, want 409 USERS_PROFILE_IMAGE_CONTENT_TYPE_MISMATCH", problem)
		}
		if len(repo.markedDeleted) != 1 || len(objectStorage.deleted) != 1 {
			t.Error("rejected upload not cleaned up")
		}
	})
}

func TestCompleteUploadRequiresStorage(t *testing.T) {
	service, _, _ := newImageFixture(false)
	problem := problemError(t, service.CompleteUpload(context.Background(), app.CompleteUploadInput{UserID: "user-1", FileID: "file-1"}))
	if problem.Status != http.StatusNotImplemented || problem.Code != httpx.CodeUsersObjectStorageNotConfigured {
		t.Fatalf("problem = %+v, want 501 USERS_OBJECT_STORAGE_NOT_CONFIGURED", problem)
	}
}

func TestCompleteUploadReplacesPreviousImage(t *testing.T) {
	service, repo, objectStorage, fileID := uploadFixture(t)
	repo.attachPrev = "users/user-1/profile-images/previous"
	if err := service.CompleteUpload(context.Background(), app.CompleteUploadInput{UserID: "user-1", FileID: fileID}); err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}
	if len(objectStorage.deleted) != 1 || objectStorage.deleted[0] != repo.attachPrev {
		t.Errorf("deleted = %v, want the replaced object", objectStorage.deleted)
	}
}

func TestClearProfileImage(t *testing.T) {
	service, repo, objectStorage := newImageFixture(true)
	repo.clearKey = "users/user-1/profile-images/file-1"
	if err := service.ClearProfileImage(context.Background(), app.ClearProfileImageInput{UserID: "user-1"}); err != nil {
		t.Fatalf("ClearProfileImage() error = %v", err)
	}
	if len(objectStorage.deleted) != 1 || objectStorage.deleted[0] != repo.clearKey {
		t.Errorf("deleted = %v, want the cleared object", objectStorage.deleted)
	}

	repo.clearFound = false
	problem := problemError(t, service.ClearProfileImage(context.Background(), app.ClearProfileImageInput{UserID: "missing"}))
	if problem.Status != http.StatusUnauthorized || problem.Code != httpx.CodeUnauthorized {
		t.Fatalf("problem = %+v, want 401 UNAUTHORIZED", problem)
	}
}

func TestProfileImageURL(t *testing.T) {
	service, repo, objectStorage := newImageFixture(true)
	repo.imageState = ports.ProfileImageState{FileID: "file-1", ObjectKey: "users/user-1/profile-images/file-1"}
	url, hasImage, err := service.ProfileImageURL(context.Background(), "user-1")
	if err != nil || !hasImage {
		t.Fatalf("ProfileImageURL() = %v/%t/%v, want a URL", url, hasImage, err)
	}
	if len(objectStorage.gets) != 1 || url.ExpiresAt.IsZero() {
		t.Errorf("gets = %v expiresAt = %v, want a presigned read with an expiry", objectStorage.gets, url.ExpiresAt)
	}

	repo.imageState = ports.ProfileImageState{}
	url, hasImage, err = service.ProfileImageURL(context.Background(), "user-1")
	if err != nil || hasImage || url.URL != "" {
		t.Fatalf("no-image call = %v/%t/%v, want the 204 case", url, hasImage, err)
	}

	disabled, disabledRepo, _ := newImageFixture(false)
	disabledRepo.imageState = ports.ProfileImageState{FileID: "file-1", ObjectKey: "key"}
	problem := problemError(t, mustImageURLError(disabled.ProfileImageURL(context.Background(), "user-1")))
	if problem.Status != http.StatusNotImplemented || problem.Code != httpx.CodeUsersObjectStorageNotConfigured {
		t.Fatalf("problem = %+v, want 501 with an image and no storage", problem)
	}
	disabledRepo.imageState = ports.ProfileImageState{}
	if _, hasImage, err := disabled.ProfileImageURL(context.Background(), "user-1"); err != nil || hasImage {
		t.Fatalf("no-image + disabled = %t/%v, want the 204 case before the gate", hasImage, err)
	}
}

func mustImageURLError(_ app.ImageURL, _ bool, err error) error {
	return fmt.Errorf("call: %w", err)
}

func TestCreateUploadPlanSurfacesRepositoryFailure(t *testing.T) {
	service, repo, _ := newImageFixture(true)
	repo.createFileFound = false
	problem := problemError(t, mustUploadPlanError(service.CreateUploadPlan(context.Background(), app.UploadPlanInput{
		UserID: "missing", ContentType: "image/png", SizeBytes: 10,
	})))
	if problem.Status != http.StatusUnauthorized || problem.Code != httpx.CodeUnauthorized {
		t.Fatalf("problem = %+v, want 401 for a missing owner", problem)
	}
}

func newPushFixture(enabled bool) (*app.Service, *fakeRepository) {
	repo := &fakeRepository{
		updateFound: true,
		statusFound: true,
		statusValue: domain.StatusActive,
		pushFound:   true,
	}
	service := app.New(app.Config{PushEnabled: enabled}, app.Deps{Repository: repo, Clock: clock.Fixed{Moment: moment}})
	return service, repo
}

func TestUpsertPushTokenRequiresConfiguredPush(t *testing.T) {
	service, repo := newPushFixture(false)
	problem := problemError(t, service.UpsertPushToken(context.Background(), app.UpsertPushTokenInput{
		UserID: "user-1", SessionID: "session-1", Platform: "ANDROID", Token: "fcm-token",
	}))
	if problem.Status != http.StatusNotImplemented || problem.Code != httpx.CodeAuthPushNotConfigured {
		t.Fatalf("problem = %+v, want 501 AUTH_PUSH_NOT_CONFIGURED", problem)
	}
	if len(repo.pushUpserts) != 0 {
		t.Error("push token stored although push is not configured")
	}
}

func TestUpsertPushTokenValidatesBeforeTheGate(t *testing.T) {
	service, _ := newPushFixture(false)
	problem := problemError(t, service.UpsertPushToken(context.Background(), app.UpsertPushTokenInput{
		UserID: "user-1", SessionID: "session-1", Platform: "WATCH", Token: "fcm-token",
	}))
	if problem.Status != http.StatusBadRequest || problem.Code != httpx.CodeValidationFailed {
		t.Fatalf("problem = %+v, want 400 VALIDATION_FAILED before the 501 gate", problem)
	}
}

func TestUpsertPushTokenNormalizesAndStores(t *testing.T) {
	service, repo := newPushFixture(true)
	if err := service.UpsertPushToken(context.Background(), app.UpsertPushTokenInput{ //nolint:gosec // test fixture, not a credential
		UserID: "user-1", SessionID: "session-1", Platform: " ios ", Token: "  fcm-token  ",
	}); err != nil {
		t.Fatalf("UpsertPushToken() error = %v", err)
	}
	if len(repo.pushUpserts) != 1 {
		t.Fatalf("upserts = %d, want 1", len(repo.pushUpserts))
	}
	upsert := repo.pushUpserts[0]
	if upsert.Platform != domain.PushPlatformIOS || upsert.Token != "fcm-token" {
		t.Errorf("upsert = %+v, want the normalized platform and trimmed token", upsert)
	}
	if !upsert.Now.Equal(moment) {
		t.Errorf("now = %v, want the injected clock", upsert.Now)
	}
}

func TestUpsertPushTokenValidation(t *testing.T) {
	longToken := strings.Repeat("a", pushTokenMaxLengthForTest+1)
	cases := []struct {
		name     string
		platform string
		token    string
		field    string
		message  string
	}{
		{name: "unknown platform", platform: "WATCH", token: "fcm", field: "platform", message: `must be one of "ANDROID", "IOS", "WEB"`},
		{name: "blank token", platform: "ANDROID", token: "   ", field: "token", message: "must not be empty"},
		{name: "over long token", platform: "WEB", token: longToken, field: "token", message: "must be at most 2048 characters"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, repo := newPushFixture(true)
			problem := problemError(t, service.UpsertPushToken(context.Background(), app.UpsertPushTokenInput{
				UserID: "user-1", SessionID: "session-1", Platform: tc.platform, Token: tc.token,
			}))
			if problem.Status != http.StatusBadRequest || problem.Code != httpx.CodeValidationFailed {
				t.Fatalf("problem = %+v, want 400 VALIDATION_FAILED", problem)
			}
			if len(problem.Issues) != 1 || problem.Issues[0].Field != tc.field || problem.Issues[0].Message != tc.message {
				t.Fatalf("issues = %+v, want %s %q", problem.Issues, tc.field, tc.message)
			}
			if len(repo.pushUpserts) != 0 {
				t.Error("repository called for an invalid token")
			}
		})
	}
}

const pushTokenMaxLengthForTest = 2048

func TestUpsertPushTokenRejectsDeadSessionsAndUsers(t *testing.T) {
	dead, deadRepo := newPushFixture(true)
	deadRepo.pushFound = false
	problem := problemError(t, dead.UpsertPushToken(context.Background(), app.UpsertPushTokenInput{
		UserID: "user-1", SessionID: "revoked", Platform: "ANDROID", Token: "fcm",
	}))
	if problem.Status != http.StatusUnauthorized || problem.Code != httpx.CodeUnauthorized {
		t.Fatalf("dead session mapped to %d/%s, want 401 UNAUTHORIZED", problem.Status, problem.Code)
	}

	deleted, deletedRepo := newPushFixture(true)
	deletedRepo.statusValue = domain.StatusDeleted
	problem = problemError(t, deleted.UpsertPushToken(context.Background(), app.UpsertPushTokenInput{
		UserID: "deleted", SessionID: "session-1", Platform: "ANDROID", Token: "fcm",
	}))
	if problem.Status != http.StatusUnauthorized || problem.Code != httpx.CodeUnauthorized {
		t.Fatalf("deleted user mapped to %d/%s, want 401 UNAUTHORIZED", problem.Status, problem.Code)
	}
	if len(deletedRepo.pushUpserts) != 0 {
		t.Error("push token stored for a deleted user")
	}
}

func TestRevokePushToken(t *testing.T) {
	service, repo := newPushFixture(false)
	if err := service.RevokePushToken(context.Background(), app.RevokePushTokenInput{UserID: "user-1", SessionID: "session-1"}); err != nil {
		t.Fatalf("RevokePushToken() error = %v, want nil without a live session", err)
	}
	if len(repo.pushRevokes) != 1 || repo.pushRevokes[0] != "session-1" {
		t.Errorf("revokes = %v, want session-1", repo.pushRevokes)
	}
	if !repo.pushNow.Equal(moment) {
		t.Errorf("now = %v, want the injected clock", repo.pushNow)
	}

	deleted, deletedRepo := newPushFixture(false)
	deletedRepo.statusValue = domain.StatusDeleted
	problem := problemError(t, deleted.RevokePushToken(context.Background(), app.RevokePushTokenInput{UserID: "deleted", SessionID: "session-1"}))
	if problem.Status != http.StatusUnauthorized || problem.Code != httpx.CodeUnauthorized {
		t.Fatalf("deleted user mapped to %d/%s, want 401 UNAUTHORIZED", problem.Status, problem.Code)
	}
	if len(deletedRepo.pushRevokes) != 0 {
		t.Error("push token revoked for a deleted user")
	}
}

func TestListSessionsComputesStatusAndCurrent(t *testing.T) {
	service, repo := sessionsFixture()
	repo.listPage = ports.SessionPage{
		Sessions: []domain.Session{
			{ID: "current", ExpiresAt: moment.Add(time.Hour)},
			{ID: "expired", ExpiresAt: moment.Add(-time.Minute)},
			{ID: "revoked", ExpiresAt: moment.Add(time.Hour), RevokedAt: moment.Add(-time.Hour)},
			{ID: "revoked-expired", ExpiresAt: moment.Add(-time.Hour), RevokedAt: moment.Add(-2 * time.Hour)},
		},
		HasMore:    true,
		NextCursor: "cursor-1",
	}
	page, err := service.ListSessions(context.Background(), app.ListSessionsInput{
		UserID: "user-1", SessionID: "current",
		Query: httpx.ListQuery{Limit: 2, Sort: []httpx.SortField{{Field: "createdAt", Desc: true}}},
	})
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(page.Views) != 4 {
		t.Fatalf("views = %d, want 4", len(page.Views))
	}
	want := map[string]struct {
		status  string
		current bool
	}{
		"current":         {status: domain.SessionActive, current: true},
		"expired":         {status: domain.SessionExpired},
		"revoked":         {status: domain.SessionRevoked},
		"revoked-expired": {status: domain.SessionRevoked},
	}
	for _, view := range page.Views {
		expected := want[view.ID]
		if view.Status != expected.status || view.Current != expected.current {
			t.Errorf("view %s = %s/current %t, want %s/current %t", view.ID, view.Status, view.Current, expected.status, expected.current)
		}
	}
	if !page.HasMore || page.NextCursor != "cursor-1" {
		t.Errorf("page meta = hasMore %t cursor %q, want the repository page", page.HasMore, page.NextCursor)
	}
	if len(repo.lists) != 1 || repo.lists[0].Limit != 2 || repo.lists[0].Sort[0].Field != "createdAt" {
		t.Errorf("repo input = %+v, want the parsed query", repo.lists)
	}
}

func TestListSessionsRejectsMissingAndDeletedUsers(t *testing.T) {
	missing, missingRepo := sessionsFixture()
	missingRepo.statusFound = false
	problem := problemError(t, mustSessionsError(missing.ListSessions(context.Background(), app.ListSessionsInput{UserID: "missing"})))
	if problem.Status != http.StatusUnauthorized || problem.Code != httpx.CodeUnauthorized {
		t.Fatalf("missing user mapped to %d/%s, want 401 UNAUTHORIZED", problem.Status, problem.Code)
	}
	if len(missingRepo.lists) != 0 {
		t.Error("sessions listed for a missing user")
	}

	deleted, deletedRepo := sessionsFixture()
	deletedRepo.statusValue = domain.StatusDeleted
	problem = problemError(t, mustSessionsError(deleted.ListSessions(context.Background(), app.ListSessionsInput{UserID: "deleted"})))
	if problem.Status != http.StatusUnauthorized || problem.Code != httpx.CodeUnauthorized {
		t.Fatalf("deleted user mapped to %d/%s, want 401 UNAUTHORIZED", problem.Status, problem.Code)
	}
	if len(deletedRepo.lists) != 0 {
		t.Error("sessions listed for a deleted user")
	}
}

func TestRevokeSessionOutcomes(t *testing.T) {
	t.Run("revokes", func(t *testing.T) {
		service, repo := sessionsFixture()
		repo.revokeOK = true
		if err := service.RevokeSession(context.Background(), app.RevokeSessionInput{UserID: "user-1", SessionID: "session-1"}); err != nil {
			t.Fatalf("RevokeSession() error = %v", err)
		}
		if len(repo.revocations) != 1 || repo.revocations[0] != "session-1" {
			t.Errorf("revocations = %v, want session-1", repo.revocations)
		}
		if !repo.revokeNow.Equal(moment) {
			t.Errorf("now = %v, want the injected clock", repo.revokeNow)
		}
	})

	t.Run("unknown session is not found", func(t *testing.T) {
		service, repo := sessionsFixture()
		repo.revokeOK = false
		problem := problemError(t, service.RevokeSession(context.Background(), app.RevokeSessionInput{UserID: "user-1", SessionID: "missing"}))
		if problem.Status != http.StatusNotFound || problem.Code != httpx.CodeNotFound {
			t.Fatalf("problem = %+v, want 404 NOT_FOUND", problem)
		}
	})

	t.Run("deleted user is unauthorized", func(t *testing.T) {
		service, repo := sessionsFixture()
		repo.statusValue = domain.StatusDeleted
		problem := problemError(t, service.RevokeSession(context.Background(), app.RevokeSessionInput{UserID: "deleted", SessionID: "session-1"}))
		if problem.Status != http.StatusUnauthorized || problem.Code != httpx.CodeUnauthorized {
			t.Fatalf("problem = %+v, want 401 UNAUTHORIZED", problem)
		}
		if len(repo.revocations) != 0 {
			t.Error("revoke attempted for a deleted user")
		}
	})
}
