// Package job defines the data structures for parallel job execution.
package job

// Status represents the outcome of a job execution.
type Status int

const (
	// StatusSuccess indicates a job completed successfully.
	StatusSuccess Status = iota
	// StatusFailure indicates a job failed during execution.
	StatusFailure
)

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
