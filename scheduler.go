package browserpm

import "sync/atomic"

// Scheduler picks the next poolPage from a set of available pages.
// Implementations must be safe for concurrent use.
type Scheduler interface {
	Select(pages []*poolPage) *poolPage
	Reset()
}

// RoundRobinScheduler distributes requests evenly across pages.
type RoundRobinScheduler struct {
	counter atomic.Uint64
}

func (s *RoundRobinScheduler) Select(pages []*poolPage) *poolPage {
	n := uint64(len(pages))
	if n == 0 {
		return nil
	}
	idx := s.counter.Add(1) - 1
	return pages[idx%n]
}

func (s *RoundRobinScheduler) Reset() {
	s.counter.Store(0)
}

// NewScheduler creates a Scheduler by strategy name.
// Unrecognised strategies fall back to round-robin.
func NewScheduler(strategy string) Scheduler {
	switch strategy {
	case "round-robin":
		return &RoundRobinScheduler{}
	default:
		return &RoundRobinScheduler{}
	}
}
