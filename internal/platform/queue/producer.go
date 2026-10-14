package queue

import (
	"context"
	"fmt"

	"github.com/hibiken/asynq"
)

// Producer enqueues typed jobs with the platform defaults.
type Producer struct {
	client *asynq.Client
}

// NewProducer builds the producer from parsed Redis options. It does not dial.
func NewProducer(options asynq.RedisClientOpt) *Producer {
	return &Producer{client: asynq.NewClient(options)}
}

// Close releases the producer's connection.
func (p *Producer) Close() error {
	if err := p.client.Close(); err != nil {
		return fmt.Errorf("queue: close producer: %w", err)
	}
	return nil
}

// Enqueue adds a job to a queue with the metadata envelope and the platform
// retry policy. Extra asynq options (delay, uniqueness) are appended.
func (p *Producer) Enqueue(ctx context.Context, queueName, jobName string, payload any, meta Meta, extra ...asynq.Option) error {
	queue, err := Name(queueName)
	if err != nil {
		return err
	}
	job, err := JobName(jobName)
	if err != nil {
		return err
	}
	encoded, err := WithMeta(payload, meta)
	if err != nil {
		return err
	}
	options := []asynq.Option{
		asynq.Queue(queue),
		asynq.MaxRetry(MaxRetries),
		asynq.Timeout(TaskTimeout),
	}
	options = append(options, extra...)
	task := asynq.NewTask(job, encoded, options...)
	if _, err := p.client.EnqueueContext(ctx, task); err != nil {
		return fmt.Errorf("queue: enqueue %s: %w", job, err)
	}
	return nil
}
