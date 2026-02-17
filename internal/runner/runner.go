// Package runner provides the parallel job execution engine.
package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/ivoronin/pll/internal/checkpoint"
	"github.com/ivoronin/pll/internal/job"
	"github.com/ivoronin/pll/internal/output"
)

// Config holds settings for a parallel execution run.
type Config struct {
	Jobs        int
	Checkpoint  *checkpoint.Store
	Interactive bool
	Output      *output.Factory
}

// Summary tracks the outcome counts of a completed run.
type Summary struct {
	Total      int
	Succeeded  int
	Failed     int
	Skipped    int
	FailedJobs []*job.Job
}

type runState struct {
	cfg     Config
	summary *Summary
	mutex   sync.Mutex
	runErr  error
}

func (rs *runState) processJob(ctx context.Context, currentJob *job.Job) {
	result := execute(ctx, rs.cfg, currentJob)

	rs.mutex.Lock()
	if result.Status == job.StatusSuccess {
		rs.summary.Succeeded++
	} else {
		rs.summary.Failed++
		rs.summary.FailedJobs = append(rs.summary.FailedJobs, currentJob)
	}
	rs.mutex.Unlock()

	rs.cfg.Output.IncProgress()

	if rs.cfg.Checkpoint != nil {
		recordErr := rs.cfg.Checkpoint.Record(currentJob, result)
		if recordErr != nil {
			rs.mutex.Lock()
			rs.runErr = errors.Join(rs.runErr, fmt.Errorf("checkpoint record: %w", recordErr))
			rs.mutex.Unlock()
		}
	}
}

// Run executes all jobs in parallel according to the given configuration.
func Run(ctx context.Context, cfg Config, jobs []*job.Job) (*Summary, error) {
	state := &runState{
		cfg:     cfg,
		summary: &Summary{Total: len(jobs)},
	}

	sem := make(chan struct{}, cfg.Jobs)

	var waitGroup sync.WaitGroup

	for _, currentJob := range jobs {
		if ctx.Err() != nil {
			break
		}

		if cfg.Checkpoint != nil {
			shouldRun, checkErr := cfg.Checkpoint.ShouldRun(currentJob)
			if checkErr != nil {
				return state.summary, fmt.Errorf("checkpoint check: %w", checkErr)
			}

			if !shouldRun {
				state.mutex.Lock()
				state.summary.Skipped++
				state.mutex.Unlock()

				state.cfg.Output.IncProgress()

				continue
			}
		}

		sem <- struct{}{}

		if ctx.Err() != nil {
			<-sem

			break
		}

		waitGroup.Add(1)

		go func(currentJob *job.Job) {
			defer waitGroup.Done()
			defer func() { <-sem }()

			state.processJob(ctx, currentJob)
		}(currentJob)
	}

	waitGroup.Wait()

	return state.summary, state.runErr
}

func execute(ctx context.Context, cfg Config, currentJob *job.Job) job.Result {
	//nolint:gosec // pll executes user-provided shell commands by design
	cmd := exec.CommandContext(ctx, "sh", "-c", currentJob.Command)

	writers := cfg.Output.NewWriters()
	cmd.Stdout = writers.Stdout
	cmd.Stderr = writers.Stderr

	defer writers.Flush()

	if currentJob.Dir != "" {
		cmd.Dir = currentJob.Dir
	}

	if cfg.Interactive {
		tty, ttyErr := os.Open("/dev/tty")
		if ttyErr == nil {
			defer func() { _ = tty.Close() }()

			cmd.Stdin = tty
		}
	}

	runErr := cmd.Run()
	if runErr != nil {
		exitCode := 1

		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		}

		return job.Result{Status: job.StatusFailure, ExitCode: exitCode}
	}

	return job.Result{Status: job.StatusSuccess, ExitCode: 0}
}
