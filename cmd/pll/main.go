// Command pll runs shell commands in parallel with output buffering and checkpoints.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime"

	"github.com/ivoronin/pll/internal/checkpoint"
	"github.com/ivoronin/pll/internal/command"
	"github.com/ivoronin/pll/internal/job"
	"github.com/ivoronin/pll/internal/output"
	"github.com/ivoronin/pll/internal/runner"
	flag "github.com/spf13/pflag"
)

var (
	version = "dev"

	errConflictingJobsFlag  = errors.New("--interactive and --jobs are mutually exclusive")
	errConflictingBufferFlag = errors.New("--interactive and --buffer are mutually exclusive")
)

func main() {
	os.Exit(run())
}

func run() int {
	versionFlag := flag.Bool("version", false, "print version and exit")
	interactive := flag.BoolP("interactive", "i", false, "run jobs sequentially with stdin connected")
	jobs := flag.IntP("jobs", "j", runtime.NumCPU(), "number of parallel jobs")
	checkpointPath := flag.StringP("checkpoint", "c", "", "path to checkpoint file")
	bufferMode := flag.StringP("buffer", "b", "line", "output buffering mode: none, line, job")
	chdir := flag.StringP("chdir", "C", "", "change to directory before running command (supports {} placeholder)")

	flag.Parse()

	if *versionFlag {
		_, _ = fmt.Fprintf(os.Stdout, "pll %s\n", version)

		return 0
	}

	interactiveErr := resolveInteractive(*interactive, jobs, bufferMode)
	if interactiveErr != nil {
		fmt.Fprintf(os.Stderr, "pll: %v\n", interactiveErr)

		return 2
	}

	chdirErr := resolveChdir(chdir)
	if chdirErr != nil {
		fmt.Fprintf(os.Stderr, "pll: %v\n", chdirErr)

		return 1
	}

	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: pll [flags] <command template>")
		flag.PrintDefaults()

		return 2
	}

	allJobs, err := readJobs(flag.Arg(0), *chdir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pll: %v\n", err)

		return 1
	}

	if len(allJobs) == 0 {
		return 0
	}

	summary, runErr := executeJobs(allJobs, *jobs, *interactive, *bufferMode, *checkpointPath)
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "pll: %v\n", runErr)
	}

	for _, j := range summary.FailedJobs {
		fmt.Fprintf(os.Stderr, "pll: job failed: '%s' in '%s'\n", j.Command, j.Dir)
	}

	fmt.Fprintf(os.Stderr, "pll: %d succeeded, %d failed, %d skipped (total: %d)\n",
		summary.Succeeded, summary.Failed, summary.Skipped, summary.Total)

	if summary.Failed > 0 || runErr != nil {
		return 1
	}

	return 0
}

func resolveInteractive(interactive bool, jobs *int, bufferMode *string) error {
	if !interactive {
		return nil
	}

	if flag.Lookup("jobs").Changed {
		return errConflictingJobsFlag
	}

	if flag.Lookup("buffer").Changed {
		return errConflictingBufferFlag
	}

	*jobs = 1
	*bufferMode = string(output.ModeNone)

	return nil
}

func resolveChdir(chdir *string) error {
	if *chdir != "" {
		return nil
	}

	cwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		return cwdErr
	}

	*chdir = cwd

	return nil
}

func readJobs(commandTemplate string, chdir string) ([]*job.Job, error) {
	tmpl := command.NewTemplate(commandTemplate, chdir)

	var allJobs []*job.Job

	scanner := bufio.NewScanner(os.Stdin)

	for scanner.Scan() {
		allJobs = append(allJobs, tmpl.Expand(scanner.Text()))
	}

	scanErr := scanner.Err()
	if scanErr != nil {
		return nil, fmt.Errorf("reading stdin: %w", scanErr)
	}

	return allJobs, nil
}

func executeJobs(
	allJobs []*job.Job,
	jobCount int,
	interactive bool,
	bufferMode string,
	checkpointPath string,
) (*runner.Summary, error) {
	mode := output.Mode(bufferMode)

	cfg := runner.Config{
		Jobs:        jobCount,
		Interactive: interactive,
		Output:      output.NewFactory(mode),
	}

	if checkpointPath != "" {
		store, err := checkpoint.Open(checkpointPath)
		if err != nil {
			return &runner.Summary{Total: len(allJobs)}, err
		}

		defer func() {
			_ = store.Close()
		}()

		cfg.Checkpoint = store
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	return runner.Run(ctx, cfg, allJobs)
}
