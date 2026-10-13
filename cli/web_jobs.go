package main

import (
	"fmt"
	"io"
	"sort"
	"sync"
	"time"
)

// Container operations from the web UI can take minutes (pentest.sh may build
// the image or start sidecars), so each runs as a background job whose output
// the browser polls. Only one job per target runs at a time.

const (
	maxJobOutput = 512 << 10 // keep the tail of very chatty builds
	maxJobs      = 50
)

type Job struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`   // create | start | stop | remove | recreate
	Target  string `json:"target"` // container name
	Status  string `json:"status"` // running | done | failed
	Error   string `json:"error,omitempty"`
	Output  string `json:"output,omitempty"`
	Started int64  `json:"started"` // unix seconds
	Ended   int64  `json:"ended,omitempty"`
}

type jobState struct {
	mu  sync.Mutex
	seq int // creation order, for sorting and pruning
	job Job
	out []byte
}

// Write appends to the job's output, keeping at most maxJobOutput bytes.
func (s *jobState) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.out = append(s.out, p...)
	if over := len(s.out) - maxJobOutput; over > 0 {
		s.out = append([]byte(nil), s.out[over:]...)
	}
	return len(p), nil
}

// snapshot returns a copy of the job; withOutput renders the raw terminal
// output (colours, \r progress bars) into plain text.
func (s *jobState) snapshot(withOutput bool) Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.job
	if withOutput {
		j.Output = cleanLog(string(s.out))
	}
	return j
}

type jobRunner struct {
	mu   sync.Mutex
	seq  int
	jobs map[string]*jobState
}

func newJobRunner() *jobRunner {
	return &jobRunner{jobs: map[string]*jobState{}}
}

// errJobBusy is returned when target already has a running job.
type errJobBusy struct{ id string }

func (e errJobBusy) Error() string {
	return fmt.Sprintf("another operation (job %s) is still running for this container", e.id)
}

// start launches fn in the background and returns the new job immediately.
func (r *jobRunner) start(kind, target string, fn func(w io.Writer) error) (Job, error) {
	r.mu.Lock()
	for id, s := range r.jobs {
		if j := s.snapshot(false); j.Target == target && j.Status == "running" {
			r.mu.Unlock()
			return Job{}, errJobBusy{id}
		}
	}
	r.seq++
	s := &jobState{seq: r.seq, job: Job{
		ID: fmt.Sprintf("j%d", r.seq), Kind: kind, Target: target,
		Status: "running", Started: time.Now().Unix(),
	}}
	r.jobs[s.job.ID] = s
	r.pruneLocked()
	r.mu.Unlock()

	go func() {
		err := fn(s)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.job.Ended = time.Now().Unix()
		if err != nil {
			s.job.Status, s.job.Error = "failed", err.Error()
		} else {
			s.job.Status = "done"
		}
	}()
	return s.snapshot(false), nil
}

func (r *jobRunner) get(id string) (Job, bool) {
	r.mu.Lock()
	s, ok := r.jobs[id]
	r.mu.Unlock()
	if !ok {
		return Job{}, false
	}
	return s.snapshot(true), true
}

// list returns all known jobs, newest first, without their output.
func (r *jobRunner) list() []Job {
	r.mu.Lock()
	states := make([]*jobState, 0, len(r.jobs))
	for _, s := range r.jobs {
		states = append(states, s)
	}
	r.mu.Unlock()
	sort.Slice(states, func(i, j int) bool { return states[i].seq > states[j].seq })
	out := make([]Job, len(states))
	for i, s := range states {
		out[i] = s.snapshot(false)
	}
	return out
}

// pruneLocked drops the oldest finished jobs beyond maxJobs.
func (r *jobRunner) pruneLocked() {
	if len(r.jobs) <= maxJobs {
		return
	}
	var finished []*jobState
	for _, s := range r.jobs {
		if s.snapshot(false).Status != "running" {
			finished = append(finished, s)
		}
	}
	sort.Slice(finished, func(i, j int) bool { return finished[i].seq < finished[j].seq })
	for _, s := range finished {
		if len(r.jobs) <= maxJobs {
			return
		}
		delete(r.jobs, s.job.ID)
	}
}
