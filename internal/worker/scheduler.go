package worker

import (
	"context"
	"time"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
)

// Job represents a scheduled background job.
type Job struct {
	Name     string
	Interval time.Duration
	Fn       func(ctx context.Context) error
}

// Scheduler runs registered jobs at their configured intervals.
type Scheduler struct {
	jobs   []Job
	db     *database.PostgresDB
	redis  *database.RedisClient
	cancel context.CancelFunc
}

// NewScheduler creates a new Scheduler.
func NewScheduler(db *database.PostgresDB, redis *database.RedisClient) *Scheduler {
	return &Scheduler{
		db:    db,
		redis: redis,
	}
}

// Register adds a job to the scheduler.
func (s *Scheduler) Register(job Job) {
	s.jobs = append(s.jobs, job)
}

// Start begins all registered jobs. Each job runs in its own goroutine.
// The provided context is used as the parent; call Stop to shut down.
func (s *Scheduler) Start(ctx context.Context) {
	childCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	log := observability.Log

	for _, job := range s.jobs {
		j := job // capture loop variable
		go func() {
			log.Info().Str("job", j.Name).Dur("interval", j.Interval).Msg("starting job")
			ticker := time.NewTicker(j.Interval)
			defer ticker.Stop()

			for {
				select {
				case <-childCtx.Done():
					log.Info().Str("job", j.Name).Msg("job stopped")
					return
				case <-ticker.C:
					func() {
						defer func() {
							if r := recover(); r != nil {
								log.Error().Str("job", j.Name).Interface("panic", r).Msg("job panicked, recovering")
							}
						}()

						if err := j.Fn(childCtx); err != nil {
							log.Error().Str("job", j.Name).Err(err).Msg("job returned error")
						} else {
							log.Debug().Str("job", j.Name).Msg("job completed successfully")
						}
					}()
				}
			}
		}()
	}
}

// Stop signals all running jobs to stop and waits for cancellation.
func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	observability.Log.Info().Msg("scheduler stopped")
}
