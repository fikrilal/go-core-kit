package app_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/fikrilal/go-core-kit/internal/features/me/app"
	"github.com/fikrilal/go-core-kit/internal/features/me/domain"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
	"github.com/fikrilal/go-core-kit/internal/platform/push"
)

type fakePushSender struct {
	messageID string
	err       error
	sent      []push.Message
}

func (f *fakePushSender) SendToToken(_ context.Context, message push.Message) (string, error) {
	f.sent = append(f.sent, message)
	if f.err != nil {
		return "", f.err
	}
	return f.messageID, nil
}

func newPushJobFixture() (*app.PushJobs, *fakeRepository, *fakePushSender) {
	repo := &fakeRepository{
		pushSessionFound: true,
		pushSession: domain.PushSession{
			ID:         "session-1",
			PushToken:  "device-token",
			ExpiresAt:  moment.Add(time.Hour),
			UserStatus: domain.StatusActive,
		},
	}
	sender := &fakePushSender{messageID: "message-1"}
	jobs := app.NewPushJobs(app.PushJobDeps{Repository: repo, Push: sender, Clock: clock.Fixed{Moment: moment}})
	return jobs, repo, sender
}

func TestPushJobSends(t *testing.T) {
	jobs, _, sender := newPushJobFixture()
	outcome, err := jobs.Send(context.Background(), "session-1",
		&push.Notification{Title: "Hi", Body: "There"}, map[string]string{"kind": "test"})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if outcome.Outcome != domain.PushOutcomeSent || outcome.MessageID != "message-1" {
		t.Fatalf("outcome = %+v, want sent with the message id", outcome)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("sends = %d, want 1", len(sender.sent))
	}
	message := sender.sent[0]
	if message.Token != "device-token" || message.Notification == nil || message.Notification.Title != "Hi" {
		t.Errorf("message = %+v, want the session token and notification", message)
	}
	if message.Data["kind"] != "test" {
		t.Errorf("data = %v, want the payload", message.Data)
	}
}

func TestPushJobSkips(t *testing.T) {
	cases := []struct {
		name    string
		session domain.PushSession
		found   bool
		reason  string
	}{
		{name: "missing session", found: false, reason: domain.PushReasonSessionNotFound},
		{name: "revoked session", found: true, session: domain.PushSession{ID: "s", PushToken: "t", Revoked: true, ExpiresAt: moment.Add(time.Hour), UserStatus: domain.StatusActive}, reason: domain.PushReasonSessionRevoked},
		{name: "expired session", found: true, session: domain.PushSession{ID: "s", PushToken: "t", ExpiresAt: moment.Add(-time.Minute), UserStatus: domain.StatusActive}, reason: domain.PushReasonSessionExpired},
		{name: "inactive user", found: true, session: domain.PushSession{ID: "s", PushToken: "t", ExpiresAt: moment.Add(time.Hour), UserStatus: domain.StatusDeleted}, reason: domain.PushReasonUserInactive},
		{name: "no token", found: true, session: domain.PushSession{ID: "s", ExpiresAt: moment.Add(time.Hour), UserStatus: domain.StatusActive}, reason: domain.PushReasonNoToken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			jobs, repo, sender := newPushJobFixture()
			repo.pushSession = tc.session
			repo.pushSessionFound = tc.found
			outcome, err := jobs.Send(context.Background(), "session-1", nil, nil)
			if err != nil {
				t.Fatalf("Send() error = %v", err)
			}
			if outcome.Outcome != domain.PushOutcomeSkipped || outcome.Reason != tc.reason {
				t.Fatalf("outcome = %+v, want skipped/%s", outcome, tc.reason)
			}
			if len(sender.sent) != 0 {
				t.Error("a skipped job sent a push")
			}
		})
	}
}

func TestPushJobClearsInvalidToken(t *testing.T) {
	jobs, repo, sender := newPushJobFixture()
	sender.err = &push.SendError{
		Provider:     push.Provider,
		Code:         push.CodeInvalidToken,
		ProviderCode: "UNREGISTERED",
	}

	outcome, err := jobs.Send(context.Background(), "session-1", nil, nil)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if outcome.Outcome != domain.PushOutcomeSkipped || outcome.Reason != domain.PushReasonInvalidToken {
		t.Fatalf("outcome = %+v, want skipped/%s", outcome, domain.PushReasonInvalidToken)
	}
	if outcome.ProviderCode != "UNREGISTERED" {
		t.Errorf("provider code = %q, want UNREGISTERED", outcome.ProviderCode)
	}
	if len(repo.clearedPush) != 1 || repo.clearedPush[0] != "device-token" {
		t.Errorf("cleared = %v, want the sent token", repo.clearedPush)
	}
}

func TestPushJobPropagatesOtherFailures(t *testing.T) {
	cases := []struct {
		name string
		err  *push.SendError
	}{
		{name: "retryable", err: &push.SendError{Provider: push.Provider, Code: push.CodeSendFailed, Retryable: true}},
		{name: "non-retryable send failure", err: &push.SendError{Provider: push.Provider, Code: push.CodeSendFailed}},
		{name: "provider down", err: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			jobs, repo, sender := newPushJobFixture()
			if tc.err != nil {
				sender.err = tc.err
			} else {
				sender.err = fmt.Errorf("provider down")
			}
			if _, err := jobs.Send(context.Background(), "session-1", nil, nil); err == nil {
				t.Fatal("Send() = nil, want the failure to propagate for the retry policy")
			}
			if len(repo.clearedPush) != 0 {
				t.Error("a non-invalid-token failure cleared the stored token")
			}
		})
	}
}
