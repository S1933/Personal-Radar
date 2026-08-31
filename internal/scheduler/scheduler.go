package scheduler

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"
)

// Spec describes a recurring job. Exactly one of Every or DailyAt is set.
type Spec struct {
	Every   time.Duration // fixed interval
	DailyAt string        // "HH:MM" in Loc
	Loc     *time.Location
	Run     func(ctx context.Context)
}

// Scheduler runs jobs until the context is cancelled.
type Scheduler struct {
	log  Logger
	jobs []job
}

type Logger interface {
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
}

type job struct {
	name string
	spec Spec
}

func New(log Logger) *Scheduler {
	return &Scheduler{log: log}
}

func (s *Scheduler) Add(name string, spec Spec) {
	s.jobs = append(s.jobs, job{name: name, spec: spec})
}

// Start launches one goroutine per job. First run is immediate for interval
// jobs; daily jobs wait until their next occurrence.
func (s *Scheduler) Start(ctx context.Context) {
	for _, j := range s.jobs {
		go s.loop(ctx, j)
	}
	s.log.Info("scheduler started", "jobs", len(s.jobs))
}

func (s *Scheduler) loop(ctx context.Context, j job) {
	if j.spec.Every > 0 {
		t := time.NewTicker(j.spec.Every)
		defer t.Stop()
		s.runJob(ctx, j) // immediate first run
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.runJob(ctx, j)
			}
		}
	}

	// Daily job.
	loc := j.spec.Loc
	if loc == nil {
		loc = time.UTC
	}
	for {
		next, err := nextDaily(j.spec.DailyAt, loc)
		if err != nil {
			s.log.Warn("bad daily schedule", "job", j.name, "schedule", j.spec.DailyAt, "error", err)
			return
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.runJob(ctx, j)
		}
	}
}

// runJob executes a job's function, converting a panic into a logged
// warning. Without this, a nil map write in any collector takes down
// the scheduler, the Telegram listener and the dashboard with it —
// and Docker's restart loop hides the crash in a cycle of mysterious
// restarts. With this, the offending job is logged once and the
// goroutine continues to its next tick.
func (s *Scheduler) runJob(ctx context.Context, j job) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Warn("job panicked",
				"job", j.name,
				"panic", fmt.Sprint(r),
				"stack", string(debug.Stack()))
		}
	}()
	j.spec.Run(ctx)
}

func nextDaily(hhmm string, loc *time.Location) (time.Time, error) {
	var h, m int
	if _, err := fmt.Sscanf(hhmm, "%d:%d", &h, &m); err != nil {
		return time.Time{}, err
	}
	now := time.Now().In(loc)
	next := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, loc)
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next, nil
}
