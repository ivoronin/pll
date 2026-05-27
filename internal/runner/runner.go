// Package runner provides the parallel job execution engine.
package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/ivoronin/pll/internal/checkpoint"
	"github.com/ivoronin/pll/internal/job"
	"github.com/ivoronin/pll/internal/output"
)

// Config holds settings for a parallel execution run.
type Config struct {
	Jobs        int
	Checkpoint  *checkpoint.Store
	Interactive bool
	Output      *output.Output
	Timeout     time.Duration
	FailFast    bool
}

type runState struct {
	cfg        Config
	stats      *job.Stats
	runErr     error
	runErrMu   sync.Mutex
	launchStop context.CancelFunc
}

func (rs *runState) skipIfCheckpointed(currentJob *job.Job) (bool, error) {
	if rs.cfg.Checkpoint == nil {
		return false, nil
	}

	shouldRun, checkErr := rs.cfg.Checkpoint.ShouldRun(currentJob.Dir)
	if checkErr != nil {
		return false, fmt.Errorf("checkpoint check: %w", checkErr)
	}

	if !shouldRun {
		rs.stats.RecordSkipped()
		rs.cfg.Output.RefreshProgress()

		return true, nil
	}

	return false, nil
}

func (rs *runState) recordOutcome(currentJob *job.Job, status job.Status) {
	switch status {
	case job.StatusSuccess:
		rs.stats.RecordSucceeded()
	case job.StatusTimeout:
		rs.stats.RecordTimedOut(currentJob)

		if rs.cfg.FailFast {
			rs.launchStop()
		}
	case job.StatusFailure:
		fallthrough
	default:
		rs.stats.RecordFailed(currentJob)

		if rs.cfg.FailFast {
			rs.launchStop()
		}
	}
}

func (rs *runState) processJob(execCtx context.Context, interruptCtx context.Context, currentJob *job.Job) {
	result := execute(execCtx, interruptCtx, rs.cfg, currentJob)

	rs.recordOutcome(currentJob, result.Status)
	rs.cfg.Output.RefreshProgress()

	if rs.cfg.Checkpoint != nil {
		recordErr := rs.cfg.Checkpoint.Record(currentJob.Dir, result)
		if recordErr != nil {
			rs.runErrMu.Lock()
			rs.runErr = errors.Join(rs.runErr, fmt.Errorf("checkpoint record: %w", recordErr))
			rs.runErrMu.Unlock()
		}
	}
}

// Run executes all jobs in parallel according to the given configuration.
// execCtx controls force-killing running jobs (cancelled by 3rd Ctrl+C or timeout).
// interruptCtx controls graceful interruption (cancelled by 2nd Ctrl+C, sends SIGINT).
// launchCtx controls new job launches (cancelled by 1st Ctrl+C or fail-fast).
// stats is the shared accounting object; mutated as jobs complete.
func Run(
	execCtx context.Context,
	interruptCtx context.Context,
	launchCtx context.Context,
	cfg Config,
	jobs []*job.Job,
	stats *job.Stats,
) error {
	innerLaunchCtx, innerLaunchStop := context.WithCancel(launchCtx)
	defer innerLaunchStop()

	state := &runState{
		cfg:        cfg,
		stats:      stats,
		launchStop: innerLaunchStop,
	}

	sem := make(chan struct{}, cfg.Jobs)

	var waitGroup sync.WaitGroup

	for _, currentJob := range jobs {
		if innerLaunchCtx.Err() != nil {
			break
		}

		skipped, checkErr := state.skipIfCheckpointed(currentJob)
		if checkErr != nil {
			return checkErr
		}

		if skipped {
			continue
		}

		sem <- struct{}{}

		if innerLaunchCtx.Err() != nil {
			<-sem

			break
		}

		waitGroup.Add(1)

		go func(currentJob *job.Job) {
			defer waitGroup.Done()
			defer func() { <-sem }()

			state.processJob(execCtx, interruptCtx, currentJob)
		}(currentJob)
	}

	waitGroup.Wait()

	return state.runErr
}

func execute(
	execCtx context.Context,
	interruptCtx context.Context,
	cfg Config,
	currentJob *job.Job,
) job.Result {
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc

		execCtx, cancel = context.WithTimeout(execCtx, cfg.Timeout)
		defer cancel()
	}

	//nolint:gosec // pll executes user-provided shell commands by design
	cmd := exec.CommandContext(execCtx, "sh", "-c", currentJob.Command)

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

	startErr := cmd.Start()
	if startErr != nil {
		fmt.Fprintf(writers.Stderr, "pll: failed to start: %v\n", startErr)

		return job.Result{Status: job.StatusFailure, ExitCode: 1}
	}

	done := make(chan struct{})

	go func() {
		select {
		case <-interruptCtx.Done():
			_ = cmd.Process.Signal(os.Interrupt)
		case <-done:
		}
	}()

	runErr := cmd.Wait()

	close(done)

	return buildResult(runErr, execCtx.Err())
}

func buildResult(runErr error, ctxErr error) job.Result {
	if runErr != nil {
		exitCode := 1

		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		}

		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return job.Result{Status: job.StatusTimeout, ExitCode: exitCode}
		}

		return job.Result{Status: job.StatusFailure, ExitCode: exitCode}
	}

	return job.Result{Status: job.StatusSuccess, ExitCode: 0}
}
