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

type Config struct {
	Jobs        int
	Checkpoint  *checkpoint.Store
	Interactive bool
	Output      *output.Factory
}

type Summary struct {
	Total     int
	Succeeded int
	Failed    int
	Skipped   int
}

func Run(ctx context.Context, cfg Config, jobs []*job.Job) (*Summary, error) {
	sem := make(chan struct{}, cfg.Jobs)
	var mu sync.Mutex
	summary := &Summary{Total: len(jobs)}

	var wg sync.WaitGroup
	var runErr error

	for _, j := range jobs {
		if ctx.Err() != nil {
			break
		}

		if cfg.Checkpoint != nil {
			shouldRun, err := cfg.Checkpoint.ShouldRun(j)
			if err != nil {
				return summary, fmt.Errorf("checkpoint check: %w", err)
			}
			if !shouldRun {
				mu.Lock()
				summary.Skipped++
				mu.Unlock()
				continue
			}
		}

		sem <- struct{}{}
		if ctx.Err() != nil {
			<-sem
			break
		}

		wg.Add(1)
		go func(j *job.Job) {
			defer wg.Done()
			defer func() { <-sem }()

			result := execute(ctx, cfg, j)

			mu.Lock()
			if result.Status == job.StatusSuccess {
				summary.Succeeded++
			} else {
				summary.Failed++
			}
			mu.Unlock()

			if cfg.Checkpoint != nil {
				if err := cfg.Checkpoint.Record(j, result); err != nil {
					mu.Lock()
					runErr = errors.Join(runErr, fmt.Errorf("checkpoint record: %w", err))
					mu.Unlock()
				}
			}
		}(j)
	}

	wg.Wait()
	return summary, runErr
}

func execute(ctx context.Context, cfg Config, j *job.Job) job.Result {
	cmd := exec.CommandContext(ctx, "sh", "-c", j.Command)

	w := cfg.Output.NewWriters()
	cmd.Stdout = w.Stdout
	cmd.Stderr = w.Stderr
	defer w.Flush()

	if j.Dir != "" {
		cmd.Dir = j.Dir
	}

	if cfg.Interactive {
		cmd.Stdin = os.Stdin
	}

	if err := cmd.Run(); err != nil {
		exitCode := 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		return job.Result{Status: job.StatusFailure, ExitCode: exitCode}
	}

	return job.Result{Status: job.StatusSuccess, ExitCode: 0}
}
