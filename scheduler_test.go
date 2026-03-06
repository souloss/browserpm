package browserpm

import (
	"fmt"
	"sync"
	"testing"
)

// makePoolPages creates n minimal poolPage stubs for scheduler testing.
func makePoolPages(n int) []*poolPage {
	out := make([]*poolPage, n)
	for i := 0; i < n; i++ {
		out[i] = &poolPage{id: fmt.Sprintf("test-%d", i)}
	}
	return out
}

func TestRoundRobinScheduler_Select(t *testing.T) {
	pages := makePoolPages(3)
	s := &RoundRobinScheduler{}

	// First 6 selects should cycle 0,1,2,0,1,2
	expected := []int{0, 1, 2, 0, 1, 2}
	for i, wantIdx := range expected {
		got := s.Select(pages)
		if got != pages[wantIdx] {
			t.Errorf("Select #%d: got page %v, want index %d", i, got, wantIdx)
		}
	}
}

func TestRoundRobinScheduler_SelectEmpty(t *testing.T) {
	s := &RoundRobinScheduler{}
	got := s.Select(nil)
	if got != nil {
		t.Errorf("Select(nil) = %v, want nil", got)
	}
	got = s.Select([]*poolPage{})
	if got != nil {
		t.Errorf("Select([]) = %v, want nil", got)
	}
}

func TestRoundRobinScheduler_Reset(t *testing.T) {
	pages := makePoolPages(2)
	s := &RoundRobinScheduler{}

	s.Select(pages) // counter = 1
	s.Select(pages) // counter = 2
	s.Reset()
	got := s.Select(pages)
	if got != pages[0] {
		t.Errorf("after Reset, first Select should return pages[0], got %v", got)
	}
}

func TestRoundRobinScheduler_Concurrent(t *testing.T) {
	pages := makePoolPages(5)
	s := &RoundRobinScheduler{}

	var wg sync.WaitGroup
	iters := 100
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				p := s.Select(pages)
				if p == nil {
					t.Error("Select returned nil")
				}
			}
		}()
	}
	wg.Wait()
}

func TestNewScheduler(t *testing.T) {
	s1 := NewScheduler("round-robin")
	if s1 == nil {
		t.Fatal("NewScheduler(round-robin) should not return nil")
	}
	if _, ok := s1.(*RoundRobinScheduler); !ok {
		t.Errorf("expected *RoundRobinScheduler, got %T", s1)
	}

	s2 := NewScheduler("unknown")
	if s2 == nil {
		t.Fatal("NewScheduler(unknown) should fallback to round-robin")
	}
	if _, ok := s2.(*RoundRobinScheduler); !ok {
		t.Errorf("unknown strategy should fallback to RoundRobinScheduler, got %T", s2)
	}
}
