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
	"text/tabwriter"
	"time"

	"github.com/ivoronin/pll/internal/checkpoint"
	"github.com/ivoronin/pll/internal/command"
	"github.com/ivoronin/pll/internal/job"
	"github.com/ivoronin/pll/internal/output"
	"github.com/ivoronin/pll/internal/runner"
	flag "github.com/spf13/pflag"
)

var (
	version = "dev"

	errConflictingJobsFlag           = errors.New("--interactive and --jobs are mutually exclusive")
	errConflictingBufferFlag         = errors.New("--interactive and --buffer are mutually exclusive")
	errConflictingProgressFlag       = errors.New("--interactive and --progress are mutually exclusive")
	errConflictingTimeoutFlag        = errors.New("--interactive and --timeout are mutually exclusive")
	errConflictingProgressBufferFlag = errors.New("--progress and --buffer none are mutually exclusive")
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
	progress := flag.BoolP("progress", "p", false, "show progress bar on stderr")
	timeout := flag.DurationP("timeout", "t", 0, "per-job timeout (e.g. 30s, 5m)")
	failFast := flag.Bool("fail-fast", false, "stop launching new jobs after first failure")
	dumpCheckpoint := flag.BoolP("dump-checkpoint", "d", false, "dump checkpoint contents to stdout and exit")
	chdir := flag.StringP("chdir", "C", "", "change to directory before running command (supports {} placeholder)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: pll [OPTIONS] <COMMAND>\n\n")
		fmt.Fprintf(os.Stderr,
			"COMMAND is a shell command template where {} is replaced with each input line from stdin.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if *versionFlag {
		_, _ = fmt.Fprintf(os.Stdout, "pll %s\n", version)

		return 0
	}

	if *dumpCheckpoint {
		return runDumpCheckpoint(*checkpointPath)
	}

	return runJobs(*interactive, *progress, jobs, bufferMode, chdir,
		*checkpointPath, *timeout, *failFast)
}

func runDumpCheckpoint(path string) int {
	if path == "" {
		fmt.Fprintln(os.Stderr, "pll: --dump-checkpoint requires --checkpoint/-c")

		return 2
	}

	store, openErr := checkpoint.OpenReadOnly(path)
	if openErr != nil {
		fmt.Fprintf(os.Stderr, "pll: %v\n", openErr)

		return 1
	}

	defer func() { _ = store.Close() }()

	writer := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)

	_, _ = fmt.Fprintln(writer, "STATUS\tEXIT\tDIR")

	forEachErr := store.ForEach(func(dir string, result job.Result) error {
		_, writeErr := fmt.Fprintf(writer, "%s\t%d\t%s\n", result.Status, result.ExitCode, dir)

		return writeErr
	})
	if forEachErr != nil {
		fmt.Fprintf(os.Stderr, "pll: %v\n", forEachErr)

		return 1
	}

	flushErr := writer.Flush()
	if flushErr != nil {
		fmt.Fprintf(os.Stderr, "pll: %v\n", flushErr)

		return 1
	}

	return 0
}

func runJobs(
	interactive bool,
	progress bool,
	jobs *int,
	bufferMode *string,
	chdir *string,
	checkpointPath string,
	timeout time.Duration,
	failFast bool,
) int {
	flagErr := validateFlags(interactive, progress, jobs, bufferMode)
	if flagErr != nil {
		fmt.Fprintf(os.Stderr, "pll: %v\n", flagErr)

		return 2
	}

	chdirErr := resolveChdir(chdir)
	if chdirErr != nil {
		fmt.Fprintf(os.Stderr, "pll: %v\n", chdirErr)

		return 1
	}

	if flag.NArg() != 1 {
		flag.Usage()

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

	startTime := time.Now()

	stats, runErr := executeJobs(allJobs, *jobs, interactive, *bufferMode,
		checkpointPath, progress, timeout, failFast)
	elapsed := time.Since(startTime)

	return reportResults(stats, runErr, elapsed)
}

func reportResults(stats *job.Stats, runErr error, elapsed time.Duration) int {
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "pll: %v\n", runErr)
	}

	for _, j := range stats.FailedJobs {
		fmt.Fprintf(os.Stderr, "pll: job failed: '%s' in '%s'\n", j.Command, j.Dir)
	}

	snap := stats.Snapshot()

	fmt.Fprintf(os.Stderr, "pll: %d succeeded, %d failed, %d timed out, %d skipped (total: %d) in %s\n",
		snap.Succeeded, snap.Failed, snap.TimedOut, snap.Skipped,
		snap.Total, elapsed.Round(time.Second))

	if snap.Failed > 0 || snap.TimedOut > 0 || runErr != nil {
		return 1
	}

	return 0
}

func validateFlags(interactive bool, progress bool, jobs *int, bufferMode *string) error {
	if interactive {
		err := resolveInteractive(jobs, bufferMode)
		if err != nil {
			return err
		}
	}

	if progress && *bufferMode == string(output.ModeNone) {
		return errConflictingProgressBufferFlag
	}

	return nil
}

func resolveInteractive(jobs *int, bufferMode *string) error {
	if flag.Lookup("jobs").Changed {
		return errConflictingJobsFlag
	}

	if flag.Lookup("buffer").Changed {
		return errConflictingBufferFlag
	}

	if flag.Lookup("progress").Changed {
		return errConflictingProgressFlag
	}

	if flag.Lookup("timeout").Changed {
		return errConflictingTimeoutFlag
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
	progress bool,
	timeout time.Duration,
	failFast bool,
) (*job.Stats, error) {
	mode := output.Mode(bufferMode)
	factory := output.New(mode)
	stats := job.NewStats(len(allJobs))

	if progress {
		factory.EnableProgress(stats)
		defer factory.DoneProgress()
	}

	cfg := runner.Config{
		Jobs:        jobCount,
		Interactive: interactive,
		Output:      factory,
		Timeout:     timeout,
		FailFast:    failFast,
	}

	if checkpointPath != "" {
		store, err := checkpoint.Open(checkpointPath)
		if err != nil {
			return stats, err
		}

		defer func() {
			_ = store.Close()
		}()

		cfg.Checkpoint = store
	}

	if interactive {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()

		return stats, runner.Run(ctx, ctx, ctx, cfg, allJobs, stats)
	}

	return stats, executeParallel(cfg, allJobs, stats)
}

func executeParallel(cfg runner.Config, allJobs []*job.Job, stats *job.Stats) error {
	sigCh := make(chan os.Signal, 3)

	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	execCtx, execCancel := context.WithCancel(context.Background())
	defer execCancel()

	interruptCtx, interruptCancel := context.WithCancel(context.Background())
	defer interruptCancel()

	launchCtx, launchCancel := context.WithCancel(execCtx)
	defer launchCancel()

	go func() {
		select {
		case <-sigCh:
			fmt.Fprintln(os.Stderr,
				"\npll: waiting for running jobs to finish (press Ctrl+C to interrupt)")
			launchCancel()
		case <-execCtx.Done():
			return
		}

		select {
		case <-sigCh:
			fmt.Fprintln(os.Stderr,
				"pll: interrupting running jobs (press Ctrl+C to force)")
			interruptCancel()
		case <-execCtx.Done():
			return
		}

		select {
		case <-sigCh:
			fmt.Fprintln(os.Stderr, "pll: forcing termination")
			execCancel()
		case <-execCtx.Done():
			return
		}
	}()

	return runner.Run(execCtx, interruptCtx, launchCtx, cfg, allJobs, stats)
}
