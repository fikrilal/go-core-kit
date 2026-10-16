package queue

import (
	"context"
	"time"

	"github.com/hibiken/asynq"

	"github.com/fikrilal/go-core-kit/internal/platform/clock"
)

// Scheduler enqueues delayed jobs under deterministic task ids and cancels
// them. Re-scheduling removes the pending task first, so request/cancel cycles
// can reuse the id.
type Scheduler struct {
	producer  *Producer
	inspector *Inspector
	clock     clock.Clock
}

// NewScheduler builds the scheduler from parsed Redis options.
func NewScheduler(options asynq.RedisClientOpt, clk clock.Clock) *Scheduler {
	return &Scheduler{producer: NewProducer(options), inspector: NewInspector(options), clock: clk}
}

// Close releases the producer and inspector connections.
func (s *Scheduler) Close() error {
	producerErr := s.producer.Close()
	inspectorErr := s.inspector.Close()
	if producerErr != nil {
		return producerErr
	}
	return inspectorErr
}

// Schedule enqueues job for at, under taskID, replacing any pending task.
func (s *Scheduler) Schedule(ctx context.Context, queueName, jobName, taskID string, payload any, meta Meta, at time.Time) error {
	if err := s.inspector.DeleteTask(queueName, taskID); err != nil {
		return err
	}
	delay := at.Sub(s.clock.Now())
	if delay < 0 {
		delay = 0
	}
	return s.producer.Enqueue(ctx, queueName, jobName, payload, meta,
		asynq.TaskID(taskID), asynq.ProcessIn(delay))
}

// Cancel removes a pending scheduled task; a missing task is not an error.
func (s *Scheduler) Cancel(queueName, taskID string) error {
	return s.inspector.DeleteTask(queueName, taskID)
}
