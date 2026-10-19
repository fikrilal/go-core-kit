package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/contracttest"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
	"github.com/fikrilal/go-core-kit/internal/platform/dbtest"
	"github.com/fikrilal/go-core-kit/internal/platform/storage/storagetest"
)

type profileImagePlan struct {
	Data struct {
		FileID string `json:"fileId"`
		Upload struct {
			Method  string            `json:"method"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"upload"`
		ExpiresAt string `json:"expiresAt"`
	} `json:"data"`
}

type profileImageURL struct {
	Data struct {
		URL       string `json:"url"`
		ExpiresAt string `json:"expiresAt"`
	} `json:"data"`
}

// withObjects starts a fake S3 endpoint and wires it into the fixture config.
func withObjects(t *testing.T, mutate func(cfg *config.Config)) (*authFixture, *storagetest.Server) {
	t.Helper()
	fake := storagetest.New("profile-images")
	t.Cleanup(fake.Close)
	fixture := newAuthFixtureWith(t, func(cfg *config.Config) {
		cfg.StorageS3Endpoint = fake.Endpoint()
		cfg.StorageS3Region = "auto"
		cfg.StorageS3Bucket = "profile-images"
		cfg.StorageS3AccessKeyID = "test-access-key"
		cfg.StorageS3SecretAccessKey = "test-secret-key"
		cfg.StorageS3ForcePathStyle = true
		if mutate != nil {
			mutate(cfg)
		}
	})
	return fixture, fake
}

func (f *authFixture) uploadPlan(t *testing.T, token, idempotencyKey string, contentType string, sizeBytes int64) (*http.Request, contracttest.Response) {
	t.Helper()
	return f.jsonRequest(t, "/v1/me/profile-image/upload", token, idempotencyKey,
		map[string]any{"contentType": contentType, "sizeBytes": sizeBytes})
}

func (f *authFixture) completeImage(t *testing.T, token, fileID string) (*http.Request, contracttest.Response) {
	t.Helper()
	return f.jsonRequest(t, "/v1/me/profile-image/complete", token, "", map[string]any{"fileId": fileID})
}

func (f *authFixture) clearImage(t *testing.T, token string) (*http.Request, contracttest.Response) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodDelete, f.baseURL+"/v1/me/profile-image", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, contracttest.Do(t, f.client, req)
}

func (f *authFixture) imageURL(t *testing.T, token string) (*http.Request, contracttest.Response) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.baseURL+"/v1/me/profile-image/url", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, contracttest.Do(t, f.client, req)
}

func mustRequest(t *testing.T, url string) *http.Request {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("read request: %v", err)
	}
	return request
}

func decodePlan(t *testing.T, body []byte) profileImagePlan {
	t.Helper()
	var plan profileImagePlan
	if err := json.Unmarshal(body, &plan); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	return plan
}

func putObject(t *testing.T, plan profileImagePlan, body string, contentType string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), plan.Data.Upload.Method, plan.Data.Upload.URL, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new upload request: %v", err)
	}
	if contentType == "" {
		contentType = plan.Data.Upload.Headers["Content-Type"]
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload status = %d, want 200", resp.StatusCode)
	}
}

func storedFileState(t *testing.T, f *authFixture, fileID string) (status string, pointer *string) {
	t.Helper()
	if err := f.database.SQL().QueryRowContext(t.Context(), dbtest.Rebind("SELECT status FROM stored_files WHERE id = ?"), fileID).Scan(&status); err != nil {
		t.Fatalf("stored file: %v", err)
	}
	err := f.database.SQL().QueryRowContext(t.Context(), dbtest.Rebind("SELECT profile_image_file_id FROM user_profiles WHERE user_id = (SELECT owner_user_id FROM stored_files WHERE id = ?)"), fileID).
		Scan(&pointer)
	if err != nil && !errors.Is(err, db.ErrNoRows) {
		t.Fatalf("profile pointer: %v", err)
	}
	return status, pointer
}

// attachedImage uploads and completes one image, returning its plan.
func attachedImage(t *testing.T, f *authFixture, token, body string) profileImagePlan {
	t.Helper()
	_, resp := f.uploadPlan(t, token, "", "image/webp", int64(len(body)))
	if resp.Status != http.StatusOK {
		t.Fatalf("upload status = %d, want 200\nbody: %s", resp.Status, resp.Body)
	}
	plan := decodePlan(t, resp.Body)
	putObject(t, plan, body, "")
	if _, resp := f.completeImage(t, token, plan.Data.FileID); resp.Status != http.StatusNoContent {
		t.Fatalf("complete status = %d, want 204\nbody: %s", resp.Status, resp.Body)
	}
	return plan
}

func TestContractUsersMeProfileImageURLAndClear(t *testing.T) {
	f, fake := withObjects(t, nil)
	registered := f.register(t, "image-clear@example.com", "")
	token := registered.Data.AccessToken
	const body = "profile-image-bytes"
	plan := attachedImage(t, f, token, body)

	req, resp := f.imageURL(t, token)
	if resp.Status != http.StatusOK {
		t.Fatalf("url status = %d, want 200\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, req, resp)
	assertGoldenShapePath(t, req, resp.Status, resp.Body)
	var view profileImageURL
	if err := json.Unmarshal(resp.Body, &view); err != nil {
		t.Fatalf("decode url: %v", err)
	}
	readResponse, err := http.DefaultClient.Do(mustRequest(t, view.Data.URL))
	if err != nil {
		t.Fatalf("read object: %v", err)
	}
	defer func() { _ = readResponse.Body.Close() }()
	buffer := new(bytes.Buffer)
	if _, err := buffer.ReadFrom(readResponse.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	if buffer.String() != body {
		t.Errorf("object body = %q, want %q", buffer.String(), body)
	}

	clearReq, resp := f.clearImage(t, token)
	if resp.Status != http.StatusNoContent {
		t.Fatalf("clear status = %d, want 204\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, clearReq, resp)
	assertGoldenShapePath(t, clearReq, resp.Status, resp.Body)
	status, pointer := storedFileState(t, f, plan.Data.FileID)
	if status != "DELETED" || pointer != nil {
		t.Fatalf("after clear = %s/%v, want DELETED and detached", status, pointer)
	}
	if _, resp := f.imageURL(t, token); resp.Status != http.StatusNoContent {
		t.Fatalf("url after clear = %d, want 204", resp.Status)
	}
	if _, resp := f.clearImage(t, token); resp.Status != http.StatusNoContent {
		t.Fatalf("repeat clear = %d, want 204", resp.Status)
	}
	if fake.Count() != 0 {
		t.Errorf("objects = %d, want the cleared object removed", fake.Count())
	}
}

func TestContractUsersMeProfileImageFlow(t *testing.T) {
	f, _ := withObjects(t, nil)
	registered := f.register(t, "image@example.com", "")
	token := registered.Data.AccessToken
	const body = "profile-image-bytes"

	req, resp := f.uploadPlan(t, token, "image-key-1", "image/webp", int64(len(body)))
	if resp.Status != http.StatusOK {
		t.Fatalf("upload status = %d, want 200\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, req, resp)
	assertGoldenShapePath(t, req, resp.Status, resp.Body)
	contracttest.AssertRequestID(t, resp)
	plan := decodePlan(t, resp.Body)
	if plan.Data.FileID == "" || plan.Data.Upload.Method != http.MethodPut {
		t.Fatalf("plan = %+v, want a PUT plan with a file id", plan.Data)
	}
	if plan.Data.Upload.Headers["Content-Type"] != "image/webp" {
		t.Errorf("headers = %v, want the declared content type", plan.Data.Upload.Headers)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, plan.Data.ExpiresAt)
	if err != nil {
		t.Fatalf("expiresAt = %q: %v", plan.Data.ExpiresAt, err)
	}
	if until := time.Until(expiresAt); until <= 9*time.Minute || until > 10*time.Minute {
		t.Errorf("expiresAt = %v, want roughly now + 10 minutes", expiresAt)
	}

	_, replay := f.uploadPlan(t, token, "image-key-1", "image/webp", int64(len(body)))
	replayedPlan := decodePlan(t, replay.Body)
	if replayedPlan.Data.FileID != plan.Data.FileID {
		t.Errorf("replay fileId = %q, want the cached %q", replayedPlan.Data.FileID, plan.Data.FileID)
	}
	if replayed := replay.Header.Get("Idempotency-Replayed"); replayed != "true" {
		t.Errorf("Idempotency-Replayed = %q, want true", replayed)
	}
	var files int
	if err := f.database.SQL().QueryRowContext(t.Context(), dbtest.Rebind("SELECT COUNT(*) FROM stored_files")).Scan(&files); err != nil {
		t.Fatalf("count files: %v", err)
	}
	if files != 1 {
		t.Errorf("stored_files = %d, want the replay to reuse the record", files)
	}

	putObject(t, plan, body, "")
	completeReq, resp := f.completeImage(t, token, plan.Data.FileID)
	if resp.Status != http.StatusNoContent {
		t.Fatalf("complete status = %d, want 204\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, completeReq, resp)
	assertGoldenShapePath(t, completeReq, resp.Status, resp.Body)
	status, pointer := storedFileState(t, f, plan.Data.FileID)
	if status != "ACTIVE" || pointer == nil || *pointer != plan.Data.FileID {
		t.Fatalf("state = %s/%v, want ACTIVE attached to the profile", status, pointer)
	}
	if _, resp := f.completeImage(t, token, plan.Data.FileID); resp.Status != http.StatusNoContent {
		t.Fatalf("repeat complete status = %d, want 204", resp.Status)
	}
}

func TestContractUsersMeProfileImageGuards(t *testing.T) {
	f := newAuthFixture(t)
	registered := f.register(t, "image-off@example.com", "")
	_, resp := f.uploadPlan(t, registered.Data.AccessToken, "", "image/png", 10)
	if resp.Status != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "USERS_OBJECT_STORAGE_NOT_CONFIGURED")
	f.suite.AssertErrorCodeDeclared(t, "users.me.profileImage.upload", "USERS_OBJECT_STORAGE_NOT_CONFIGURED")

	if _, resp := f.imageURL(t, registered.Data.AccessToken); resp.Status != http.StatusNoContent {
		t.Fatalf("url without an image = %d, want 204 before the gate", resp.Status)
	}
	if _, resp := f.clearImage(t, registered.Data.AccessToken); resp.Status != http.StatusNoContent {
		t.Fatalf("clear without an image = %d, want 204", resp.Status)
	}
	_, resp = f.completeImage(t, registered.Data.AccessToken, "0f8b9d3e-1a2b-4c3d-8e4f-5a6b7c8d9e0f")
	if resp.Status != http.StatusNotImplemented {
		t.Fatalf("complete without storage = %d, want 501\nbody: %s", resp.Status, resp.Body)
	}
}

func TestContractUsersMeProfileImageValidation(t *testing.T) {
	f, _ := withObjects(t, nil)
	registered := f.register(t, "image-invalid@example.com", "")
	token := registered.Data.AccessToken
	cases := []struct {
		name        string
		contentType string
		sizeBytes   int64
		field       string
	}{
		{name: "unknown type", contentType: "image/gif", sizeBytes: 10, field: "contentType"},
		{name: "zero size", contentType: "image/png", sizeBytes: 0, field: "sizeBytes"},
		{name: "over max", contentType: "image/png", sizeBytes: 5_000_001, field: "sizeBytes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, resp := f.uploadPlan(t, token, "", tc.contentType, tc.sizeBytes)
			if resp.Status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400\nbody: %s", resp.Status, resp.Body)
			}
			contracttest.AssertProblem(t, resp, "VALIDATION_FAILED")
			issues := decodeMeIssues(t, resp.Body)
			if len(issues.Errors) == 0 || issues.Errors[0].Field != tc.field {
				t.Errorf("errors = %+v, want the %s issue", issues.Errors, tc.field)
			}
		})
	}
}

func TestContractUsersMeProfileImageRateLimit(t *testing.T) {
	f, _ := withObjects(t, func(cfg *config.Config) { cfg.UsersProfileImageUploadUserMaxAttempts = 2 })
	registered := f.register(t, "image-limit@example.com", "")
	token := registered.Data.AccessToken
	for i := range 2 {
		if _, resp := f.uploadPlan(t, token, "", "image/png", 10); resp.Status != http.StatusOK {
			t.Fatalf("attempt %d status = %d, want 200", i, resp.Status)
		}
	}
	_, resp := f.uploadPlan(t, token, "", "image/png", 10)
	if resp.Status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "RATE_LIMITED")
	if resp.Header.Get("Retry-After") == "" {
		t.Error("Retry-After missing on the 429")
	}
	f.suite.AssertErrorCodeDeclared(t, "users.me.profileImage.upload", "RATE_LIMITED")
}

func TestContractUsersMeProfileImageCompleteErrors(t *testing.T) {
	f, _ := withObjects(t, nil)
	registered := f.register(t, "image-errors@example.com", "")
	token := registered.Data.AccessToken

	t.Run("unknown file", func(t *testing.T) {
		_, resp := f.completeImage(t, token, "0f8b9d3e-1a2b-4c3d-8e4f-5a6b7c8d9e0f")
		if resp.Status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "NOT_FOUND")
	})

	t.Run("object missing", func(t *testing.T) {
		_, resp := f.uploadPlan(t, token, "", "image/png", 10)
		plan := decodePlan(t, resp.Body)
		_, resp = f.completeImage(t, token, plan.Data.FileID)
		if resp.Status != http.StatusConflict {
			t.Fatalf("status = %d, want 409\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "USERS_PROFILE_IMAGE_NOT_UPLOADED")
		f.suite.AssertErrorCodeDeclared(t, "users.me.profileImage.complete", "USERS_PROFILE_IMAGE_NOT_UPLOADED")
	})

	t.Run("size mismatch", func(t *testing.T) {
		_, resp := f.uploadPlan(t, token, "", "image/png", 100)
		plan := decodePlan(t, resp.Body)
		putObject(t, plan, "short", "image/png")
		_, resp = f.completeImage(t, token, plan.Data.FileID)
		if resp.Status != http.StatusConflict {
			t.Fatalf("status = %d, want 409\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "USERS_PROFILE_IMAGE_SIZE_MISMATCH")
		f.suite.AssertErrorCodeDeclared(t, "users.me.profileImage.complete", "USERS_PROFILE_IMAGE_SIZE_MISMATCH")
		if status, _ := storedFileState(t, f, plan.Data.FileID); status != "DELETED" {
			t.Errorf("status = %s, want the rejected upload marked DELETED", status)
		}
	})

	t.Run("content type mismatch", func(t *testing.T) {
		_, resp := f.uploadPlan(t, token, "", "image/png", 4)
		plan := decodePlan(t, resp.Body)
		putObject(t, plan, "abcd", "image/webp")
		_, resp = f.completeImage(t, token, plan.Data.FileID)
		if resp.Status != http.StatusConflict {
			t.Fatalf("status = %d, want 409\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "USERS_PROFILE_IMAGE_CONTENT_TYPE_MISMATCH")
	})

	t.Run("foreign file", func(t *testing.T) {
		_, resp := f.uploadPlan(t, token, "", "image/png", 4)
		plan := decodePlan(t, resp.Body)
		other := f.register(t, "image-other@example.com", "")
		_, resp = f.completeImage(t, other.Data.AccessToken, plan.Data.FileID)
		if resp.Status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 for another user's file\nbody: %s", resp.Status, resp.Body)
		}
	})

	t.Run("missing token", func(t *testing.T) {
		_, resp := f.completeImage(t, "", "0f8b9d3e-1a2b-4c3d-8e4f-5a6b7c8d9e0f")
		if resp.Status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.Status)
		}
		if _, resp := f.imageURL(t, ""); resp.Status != http.StatusUnauthorized {
			t.Fatalf("url without a token = %d, want 401", resp.Status)
		}
	})
}
