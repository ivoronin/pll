package job

type Status int

const (
	StatusSuccess Status = iota
	StatusFailure
)

type Result struct {
	Status   Status `json:"status"`
	ExitCode int    `json:"exit_code"`
}

type Job struct {
	Command string
	Dir     string
}
