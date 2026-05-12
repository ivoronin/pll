package job

import (
	"sync"
	"sync/atomic"
)

// Stats tracks the outcome of a parallel job run: per-bucket counts and the
// list of failing jobs. Numeric buckets use atomics; FailedJobs is appended
// under an internal mutex.
type Stats struct {
	Total int

	Succeeded atomic.Int64
	Failed    atomic.Int64
	TimedOut  atomic.Int64
	Skipped   atomic.Int64

	failedMu   sync.Mutex
	FailedJobs []*Job
}

// NewStats creates a Stats with the given total job count.
func NewStats(total int) *Stats {
	return &Stats{Total: total}
}

// RecordSucceeded bumps the success counter.
func (s *Stats) RecordSucceeded() { s.Succeeded.Add(1) }

// RecordSkipped bumps the skipped counter.
func (s *Stats) RecordSkipped() { s.Skipped.Add(1) }

// RecordFailed bumps the failed counter and appends the job to FailedJobs.
func (s *Stats) RecordFailed(j *Job) {
	s.Failed.Add(1)
	s.appendFailed(j)
}

// RecordTimedOut bumps the timed-out counter and appends the job to FailedJobs.
func (s *Stats) RecordTimedOut(j *Job) {
	s.TimedOut.Add(1)
	s.appendFailed(j)
}

// StatsSnapshot is a point-in-time read of Stats numeric buckets.
// Reads are not strictly atomic across all four fields; for live display
// and final reporting (after WaitGroup completion) this is sufficient.
type StatsSnapshot struct {
	Total     int
	Succeeded int
	Failed    int
	TimedOut  int
	Skipped   int
}

// Snapshot reads all counter buckets and returns them as a value.
func (s *Stats) Snapshot() StatsSnapshot {
	return StatsSnapshot{
		Total:     s.Total,
		Succeeded: int(s.Succeeded.Load()),
		Failed:    int(s.Failed.Load()),
		TimedOut:  int(s.TimedOut.Load()),
		Skipped:   int(s.Skipped.Load()),
	}
}

func (s *Stats) appendFailed(j *Job) {
	s.failedMu.Lock()
	defer s.failedMu.Unlock()

	s.FailedJobs = append(s.FailedJobs, j)
}
