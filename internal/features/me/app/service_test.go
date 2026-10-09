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
	service := app.New(app.Deps{Repository: repo, Clock: clock.Fixed{Moment: moment}})
	return service, repo
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

func mustSessionsError(_ app.SessionPage, err error) error {
	return fmt.Errorf("call: %w", err)
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
