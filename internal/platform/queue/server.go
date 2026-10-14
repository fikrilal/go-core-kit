package queue

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"github.com/hibiken/asynq"
)

// HandlerFunc consumes one job.
type HandlerFunc func(ctx context.Context, task *asynq.Task) error

// Server consumes jobs with the platform retry policy.
type Server struct {
	server *asynq.Server
}

// NewServer builds the asynq server. Queues map names to priorities, and a
// handler is registered per job type through NewServeMux.
func NewServer(options asynq.RedisClientOpt, queues map[string]int, concurrency int, logger *slog.Logger) (*Server, error) {
	if len(queues) == 0 {
		return nil, fmt.Errorf("queue: at least one queue is required")
	}
	if concurrency < 1 {
		return nil, fmt.Errorf("queue: concurrency must be positive, got %d", concurrency)
	}
	validated := make(map[string]int, len(queues))
	for name, priority := range queues {
		queue, err := Name(name)
		if err != nil {
			return nil, err
		}
		if priority < 1 {
			return nil, fmt.Errorf("queue: priority for %s must be positive, got %d", queue, priority)
		}
		validated[queue] = priority
	}
	config := asynq.Config{
		Concurrency:    concurrency,
		Queues:         validated,
		RetryDelayFunc: RetryDelay,
		Logger:         slogLogger{logger: logger},
	}
	return &Server{server: asynq.NewServer(options, config)}, nil
}

// NewServeMux builds the handler registry; each job type is validated.
func NewServeMux(handlers map[string]HandlerFunc) (*asynq.ServeMux, error) {
	mux := asynq.NewServeMux()
	for jobName, handler := range handlers {
		job, err := JobName(jobName)
		if err != nil {
			return nil, err
		}
		if handler == nil {
			return nil, fmt.Errorf("queue: handler for %s is nil", job)
		}
		mux.HandleFunc(job, handler)
	}
	return mux, nil
}

// Start begins consuming jobs.
func (s *Server) Start(mux *asynq.ServeMux) error {
	if err := s.server.Start(mux); err != nil {
		return fmt.Errorf("queue: start server: %w", err)
	}
	return nil
}

// Shutdown drains in-flight jobs and stops consuming.
func (s *Server) Shutdown() {
	s.server.Shutdown()
}

// RetryDelay is the jittered exponential backoff applied to every retry.
func RetryDelay(attempts int, _ error, _ *asynq.Task) time.Duration {
	return jitteredDelay(attempts, rand.Float64)
}

// jitteredDelay is the testable core of RetryDelay: base * 2^attempts with
// ±RetryJitterRatio jitter, capped so the exponent cannot overflow.
func jitteredDelay(attempts int, random func() float64) time.Duration {
	if attempts < 0 {
		attempts = 0
	}
	if attempts > 10 {
		attempts = 10
	}
	delay := RetryBaseDelay << attempts
	jitter := 1 + RetryJitterRatio*(2*random()-1)
	return time.Duration(float64(delay) * jitter)
}

// slogLogger adapts asynq's logger to slog. Fatal logs at error without
// exiting: asynq uses it for internal logging faults, and a logging problem
// should not kill the worker mid-drain.
type slogLogger struct {
	logger *slog.Logger
}

func (l slogLogger) Debug(args ...any) { l.logger.Debug(fmt.Sprint(args...)) }
func (l slogLogger) Info(args ...any)  { l.logger.Info(fmt.Sprint(args...)) }
func (l slogLogger) Warn(args ...any)  { l.logger.Warn(fmt.Sprint(args...)) }
func (l slogLogger) Error(args ...any) { l.logger.Error(fmt.Sprint(args...)) }
func (l slogLogger) Fatal(args ...any) { l.logger.Error(fmt.Sprint(args...)) }
