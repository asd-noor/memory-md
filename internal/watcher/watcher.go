// Package watcher wraps fsnotify to monitor MEMORY_MD_DIR and call engine
// handlers with a 500 ms debounce.
//
// Only root-level .md files are watched; events for subdirectories or
// non-.md files are silently discarded.
package watcher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"memory-md/internal/engine"
)

const debounce = 500 * time.Millisecond

// eventKind distinguishes changed vs deleted events.
type eventKind int

const (
	kindChanged eventKind = iota
	kindDeleted
)

// pendingEntry tracks the debounce state for a single file path.
type pendingEntry struct {
	kind  eventKind
	timer *time.Timer
}

// Watcher monitors a directory and calls engine handlers on file events.
type Watcher struct {
	eng    *engine.Engine
	memDir string
	w      *fsnotify.Watcher
}

// New creates a Watcher but does not start it yet.
func New(eng *engine.Engine, memDir string) (*Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("watcher.New: %w", err)
	}
	if err := w.Add(memDir); err != nil {
		w.Close()
		return nil, fmt.Errorf("watcher.New add dir: %w", err)
	}
	return &Watcher{eng: eng, memDir: memDir, w: w}, nil
}

// Run starts the event loop. It blocks until the watcher is closed.
func (wt *Watcher) Run() {
	mu := sync.Mutex{}
	inflight := make(map[string]*pendingEntry)

	fire := func(name string) {
		mu.Lock()
		e, ok := inflight[name]
		if ok {
			delete(inflight, name)
		}
		mu.Unlock()
		if !ok {
			return
		}
		switch e.kind {
		case kindChanged:
			if err := wt.eng.HandleChanged(name); err != nil {
				fmt.Fprintf(os.Stderr, "memory-md watcher: %s: %v\n", name, err)
			}
		case kindDeleted:
			if err := wt.eng.HandleDeleted(name); err != nil {
				fmt.Fprintf(os.Stderr, "memory-md watcher: delete %s: %v\n", name, err)
			}
		}
	}

	schedule := func(name string, kind eventKind) {
		mu.Lock()
		defer mu.Unlock()
		if e, ok := inflight[name]; ok {
			e.timer.Stop()
			e.kind = kind
			e.timer.Reset(debounce)
			return
		}
		e := &pendingEntry{kind: kind}
		e.timer = time.AfterFunc(debounce, func() { fire(name) })
		inflight[name] = e
	}

	for {
		select {
		case event, ok := <-wt.w.Events:
			if !ok {
				return
			}
			name := event.Name
			// Only root-level .md files.
			if !strings.HasSuffix(name, ".md") {
				continue
			}
			if filepath.Dir(name) != wt.memDir {
				continue
			}
			switch {
			case event.Has(fsnotify.Create) || event.Has(fsnotify.Write):
				schedule(name, kindChanged)
			case event.Has(fsnotify.Remove):
				schedule(name, kindDeleted)
			case event.Has(fsnotify.Rename):
				// Rename means the file was moved away from this name.
				// If it no longer exists on disk, treat as deleted.
				if _, err := os.Stat(name); os.IsNotExist(err) {
					schedule(name, kindDeleted)
				}
			}
			// Chmod ignored.

		case err, ok := <-wt.w.Errors:
			if !ok {
				return
			}
			fmt.Fprintf(os.Stderr, "memory-md watcher error: %v\n", err)
		}
	}
}

// Close stops the watcher.
func (wt *Watcher) Close() error {
	return wt.w.Close()
}
