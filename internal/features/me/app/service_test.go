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
