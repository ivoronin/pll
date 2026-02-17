package main

import (
	"bufio"
	"context"
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

func main() {
	jobs := flag.IntP("jobs", "j", runtime.NumCPU(), "number of parallel jobs")
	checkpointPath := flag.StringP("checkpoint", "c", "", "path to checkpoint file")
	bufferMode := flag.StringP("buffer", "b", "line", "output buffering mode: none, line, job")
	chdir := flag.StringP("chdir", "C", "", "change to directory before running command (supports {} placeholder)")
	flag.Parse()

	if *chdir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "pll: %v\n", err)
			os.Exit(1)
		}
		*chdir = cwd
	}

	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: pll [flags] <command template>")
		flag.PrintDefaults()
		os.Exit(2)
	}

	tmpl := command.NewTemplate(flag.Arg(0), *chdir)

	var allJobs []*job.Job
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		allJobs = append(allJobs, tmpl.Expand(scanner.Text()))
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "pll: reading stdin: %v\n", err)
		os.Exit(1)
	}

	if len(allJobs) == 0 {
		os.Exit(0)
	}

	interactive := *jobs == 1
	mode := output.Mode(*bufferMode)
	if interactive {
		mode = output.ModeNone
	}

	cfg := runner.Config{
		Jobs:        *jobs,
		Interactive: interactive,
		Output:      output.NewFactory(mode),
	}

	if *checkpointPath != "" {
		store, err := checkpoint.Open(*checkpointPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "pll: %v\n", err)
			os.Exit(1)
		}
		defer store.Close()
		cfg.Checkpoint = store
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	summary, err := runner.Run(ctx, cfg, allJobs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pll: %v\n", err)
	}

	fmt.Fprintf(os.Stderr, "pll: %d succeeded, %d failed, %d skipped (total: %d)\n",
		summary.Succeeded, summary.Failed, summary.Skipped, summary.Total)

	if summary.Failed > 0 || err != nil {
		os.Exit(1)
	}
}
