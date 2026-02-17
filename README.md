# pll

Run shell commands in parallel with output buffering and resumable checkpoints

[![CI](https://github.com/ivoronin/pll/actions/workflows/test.yml/badge.svg)](https://github.com/ivoronin/pll/actions/workflows/test.yml)
[![Release](https://img.shields.io/github/v/release/ivoronin/pll)](https://github.com/ivoronin/pll/releases)

## Table of Contents

[Overview](#overview) · [Features](#features) · [Installation](#installation) · [Usage](#usage) · [Requirements](#requirements) · [License](#license)

```bash
# Before:
# cat hosts.txt | while read host; do ssh "$host" 'apt update && apt upgrade -y'; done
# (sequential, no progress tracking, start over on failure)

# After:
pll -c upgrade.db 'ssh {} "apt update && apt upgrade -y"' < hosts.txt
# (parallel, atomic output, resume from checkpoint on re-run)
```

## Overview

pll reads lines from stdin, replaces `{}` placeholders in a command template with each line, and executes the resulting commands in parallel. Output is buffered at the line level by default to prevent interleaving across concurrent jobs. An optional BoltDB-backed checkpoint file tracks which jobs succeeded, allowing interrupted runs to resume without re-executing completed work.

## Features

- Configurable parallelism, defaults to number of CPU cores
- `{}` placeholder expansion in both command and working directory templates
- Three output buffering modes: none (passthrough), line (atomic lines), job (buffer entire output)
- BoltDB checkpoint persistence for resumable execution, content-addressed by SHA256 of command and directory
- Interactive mode (`-i`) with stdin passthrough for sequential execution
- Terminal progress bar for tracking job completion (`-p`)
- Graceful interruption via Ctrl+C with in-progress job completion

## Installation

### GitHub Releases

Download from [Releases](https://github.com/ivoronin/pll/releases).

### Homebrew

```bash
brew install ivoronin/ivoronin/pll
```

## Usage

### Basic Parallel Execution

```bash
# Run a command for each line from stdin
echo -e "host1\nhost2\nhost3" | pll 'ping -c1 {}'

# Read targets from a file
pll 'curl -sS https://{}/.well-known/health' < domains.txt
```

### Controlling Parallelism

```bash
# Limit to 4 concurrent jobs
pll -j4 'scp config.yml {}:/etc/app/' < hosts.txt
```

### Progress Bar

```bash
# Show a progress bar on stderr
pll -j4 -p 'rsync -a {}:/data /backup/{}' < hosts.txt
```

### Interactive Mode

```bash
# Sequential execution with stdin connected to each process
pll -i 'ssh -t {} "sudo bash"' < hosts.txt
```

`-i` forces jobs to 1 and connects stdin to the child process. Mutually exclusive with `-j`.

### Output Buffering

```bash
# Line-level buffering (default) - atomic line output, no interleaving
pll 'docker logs {}' < containers.txt

# Job-level buffering - buffer entire output per job, flush on completion
pll -b job 'kubectl logs {}' < pods.txt

# No buffering - direct passthrough (default for -i)
pll -b none 'make -C {}' < projects.txt
```

### Checkpoints

```bash
# First run - execute all jobs, record results
pll -c deploy.db 'ansible-playbook -l {} site.yml' < hosts.txt

# Re-run - skip previously successful jobs, retry failures
pll -c deploy.db 'ansible-playbook -l {} site.yml' < hosts.txt
```

Each job is identified by a SHA256 hash of its command and working directory. Results are stored in a BoltDB file. On re-run, only previously successful jobs are skipped; failed jobs are always retried.

### Working Directory

```bash
# Change to a directory per job (supports {} placeholder)
pll -C '/srv/{}' 'git pull' < repos.txt
```

### Flags

```
-i, --interactive  run jobs sequentially with stdin connected (mutually exclusive with -j)
-j, --jobs         number of parallel jobs (default: number of CPU cores)
-p, --progress     show progress bar on stderr
-b, --buffer       output buffering mode: none, line, job (default: line)
-c, --checkpoint   path to checkpoint file for resumable execution
-C, --chdir        change to directory before running command (supports {})
    --version      print version and exit
```

## Requirements

- `sh` shell available in PATH

## License

[GPL-3.0](LICENSE)
