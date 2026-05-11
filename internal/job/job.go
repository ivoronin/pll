// Package job defines the data structures for parallel job execution.
package job

// Status represents the outcome of a job execution.
type Status int

const (
	// StatusSuccess indicates a job completed successfully.
	StatusSuccess Status = iota
	// StatusFailure indicates a job failed during execution.
	StatusFailure
	// StatusTimeout indicates a job was killed because it exceeded the per-job timeout.
	StatusTimeout
)

// String returns a short human-readable name for the status.
func (s Status) String() string {
	switch s {
	case StatusSuccess:
		return "success"
	case StatusTimeout:
		return "timeout"
	case StatusFailure:
		fallthrough
	default:
		return "failure"
	}
}

// Result holds the outcome and exit code of a completed job.
type Result struct {
	Status   Status `json:"status"`
	ExitCode int    `json:"exitCode"`
}

// Job represents a single command to execute with an optional working directory.
type Job struct {
	Command string
	Dir     string
}
