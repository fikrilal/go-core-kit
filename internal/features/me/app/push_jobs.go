package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/fikrilal/go-core-kit/internal/features/me/domain"
	"github.com/fikrilal/go-core-kit/internal/features/me/ports"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
	"github.com/fikrilal/go-core-kit/internal/platform/push"
)

// PushOutcome reports how a push job ended.
type PushOutcome struct {
	Outcome      string
	Reason       string
	MessageID    string
	ProviderCode string
}

// PushJobDeps are the push handler collaborators.
type PushJobDeps struct {
	Repository ports.Repository
	Push       ports.PushSender
	Clock      clock.Clock
}

// PushJobs implements the push.send handler.
type PushJobs struct {
	repo  ports.Repository
	push  ports.PushSender
	clock clock.Clock
}

// NewPushJobs builds the push handlers.
func NewPushJobs(deps PushJobDeps) *PushJobs {
	return &PushJobs{repo: deps.Repository, push: deps.Push, clock: deps.Clock}
}

// Send delivers one push to the session's device. Every account or session
// condition is a successful skip; a non-retryable invalid-token failure
// clears the stored token when it still matches what was sent, and any other
// failure propagates so the retry policy applies.
func (j *PushJobs) Send(ctx context.Context, sessionID string, notification *push.Notification, data map[string]string) (PushOutcome, error) {
	session, found, err := j.repo.SessionForPush(ctx, sessionID)
	if err != nil {
		return PushOutcome{}, fmt.Errorf("me push job: find session: %w", err)
	}
	if !found {
		return skippedPush(domain.PushReasonSessionNotFound), nil
	}
	if session.Revoked {
		return skippedPush(domain.PushReasonSessionRevoked), nil
	}
	if !session.ExpiresAt.After(j.clock.Now()) {
		return skippedPush(domain.PushReasonSessionExpired), nil
	}
	if session.UserStatus != domain.StatusActive {
		return skippedPush(domain.PushReasonUserInactive), nil
	}
	if session.PushToken == "" {
		return skippedPush(domain.PushReasonNoToken), nil
	}
	messageID, err := j.push.SendToToken(ctx, push.Message{
		Token:        session.PushToken,
		Notification: notification,
		Data:         data,
	})
	if err == nil {
		return PushOutcome{Outcome: domain.PushOutcomeSent, MessageID: messageID}, nil
	}
	var sendErr *push.SendError
	if errors.As(err, &sendErr) && !sendErr.Retryable && sendErr.Code == push.CodeInvalidToken {
		if clearErr := j.repo.ClearSessionPushTokenIfMatches(ctx, session.ID, session.PushToken, j.clock.Now()); clearErr != nil {
			return PushOutcome{}, fmt.Errorf("me push job: clear invalid token: %w", clearErr)
		}
		return PushOutcome{
			Outcome:      domain.PushOutcomeSkipped,
			Reason:       domain.PushReasonInvalidToken,
			ProviderCode: sendErr.ProviderCode,
		}, nil
	}
	return PushOutcome{}, fmt.Errorf("me push job: send: %w", err)
}

func skippedPush(reason string) PushOutcome {
	return PushOutcome{Outcome: domain.PushOutcomeSkipped, Reason: reason}
}
