package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gammons/slk/internal/config"
)

// TestWorkspaceConfigStore_ConcurrentSaveAndSnapshot is a race-detector
// test. It reproduces the pattern boot produces -- the Update
// goroutine's theme/sidebar-width savers calling SaveTheme while every
// connect goroutine calls Snapshot, the same shape as
// buildChannelItem/WorkspaceByTeamID reading the result afterward --
// and fails under -race against this commit's workspaceConfigStore,
// which has no synchronization: Snapshot's value copy of cfg aliases
// the same Workspaces map SaveTheme mutates. The next commit fixes it
// by adding a mutex and making Snapshot copy the map independently.
func TestWorkspaceConfigStore_ConcurrentSaveAndSnapshot(t *testing.T) {
	s := newWorkspaceConfigStore(config.Config{Workspaces: map[string]config.Workspace{}})
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("T%d", i)
		s.cfg.Workspaces[id] = config.Workspace{TeamID: id}
	}

	var writer, readers sync.WaitGroup
	var writes atomic.Int64
	stop := make(chan struct{})

	writer.Add(1)
	go func() {
		defer writer.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			s.SaveTheme("T0", "toggled")
			writes.Add(1)
		}
	}()

	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("T%d", i)
		readers.Add(1)
		go func(id string) {
			defer readers.Done()
			// The race detector only reports accesses that nothing
			// orders, so a reader must still be reading while the
			// writer writes. Rather than sleep and hope, each reader
			// keeps reading until the writer has completed another
			// save since the reader began.
			seen := writes.Load()
			for j := 0; j < 50 || writes.Load() == seen; j++ {
				snap := s.Snapshot()
				if _, ok := snap.Workspaces[id]; !ok {
					t.Errorf("snapshot missing %s", id)
				}
			}
		}(id)
	}

	readers.Wait()
	close(stop)
	writer.Wait()
}
