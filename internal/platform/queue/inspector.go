package queue

import (
	"errors"
	"fmt"

	"github.com/hibiken/asynq"
)

// Inspector cancels scheduled tasks by id. Deterministic task ids make
// cancel and re-schedule race-free: delete first, then enqueue again.
type Inspector struct {
	inspector *asynq.Inspector
}

// NewInspector builds the inspector from parsed Redis options.
func NewInspector(options asynq.RedisClientOpt) *Inspector {
	return &Inspector{inspector: asynq.NewInspector(options)}
}

// Close releases the inspector's connection.
func (i *Inspector) Close() error {
	if err := i.inspector.Close(); err != nil {
		return fmt.Errorf("queue: close inspector: %w", err)
	}
	return nil
}

// DeleteTask removes a pending or scheduled task; a missing task is not an
// error, so a cancel is safe to repeat.
func (i *Inspector) DeleteTask(queueName, taskID string) error {
	queue, err := Name(queueName)
	if err != nil {
		return err
	}
	if err := i.inspector.DeleteTask(queue, taskID); err != nil && !errors.Is(err, asynq.ErrTaskNotFound) {
		return fmt.Errorf("queue: delete task %s: %w", taskID, err)
	}
	return nil
}
