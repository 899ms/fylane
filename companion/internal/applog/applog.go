// Package applog keeps a copy of the Core's own log on disk. The desktop app
// starts the Core as a child process and wires its output to os.Stderr — and
// a packaged app has no terminal behind that, so a Core that dies after
// hours of work takes its last words with it. That happened on 2026-09-16
// and is why this exists: the log has to survive the process that wrote it.
//
// Nothing here is ever sent anywhere. The file is read by the user, or
// packed into the bundle `fylane-companion diagnostics` writes, which the
// user shares by hand or not at all.
package applog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	// DirName is the log directory inside the data directory.
	DirName = "logs"
	// latestName is the file being written now.
	latestName = "companion.log"
	// maxBytes rotates the live file once it passes this size. A Core left
	// running for days must not fill a disk; four mebibytes is far more than
	// any single session produces at info level and still small enough to
	// read in one sitting.
	maxBytes = 4 << 20
	// keep bounds how many rotated files survive. Same bound as the crash
	// directory next door, for the same reason: the data directory is the
	// user's, not a place to accumulate in.
	keep = 5
)

// Open returns a writer that copies everything to stderr and to
// <dataDir>/logs/companion.log, plus a close function. An error means the
// file could not be opened; the caller keeps its own stderr writer and logs
// on, because a Core that will not start over a log file is worse than a
// Core with no log file.
func Open(dataDir string) (io.Writer, func() error, error) {
	return OpenTo(dataDir, os.Stderr)
}

// OpenTo is Open with the second destination given explicitly, so the tee
// itself can be tested rather than assumed.
func OpenTo(dataDir string, also io.Writer) (io.Writer, func() error, error) {
	dir := filepath.Join(dataDir, DirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("creating log directory: %w", err)
	}
	path := filepath.Join(dir, latestName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("opening log file: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("measuring log file: %w", err)
	}
	w := &writer{dir: dir, path: path, file: f, size: info.Size(), also: also}
	return w, w.Close, nil
}

// writer tees one log line to both destinations and rotates the file when it
// grows past maxBytes. slog writes from whatever goroutine logged, so every
// field here is under the mutex: two interleaved writes would produce a line
// that never happened.
type writer struct {
	mu   sync.Mutex
	dir  string
	path string
	file *os.File
	size int64
	also io.Writer
}

func (w *writer) Write(p []byte) (int, error) {
	// The caller's destination first and outside the lock's failure path: a
	// disk problem must not cost the operator the line on their terminal.
	if w.also != nil {
		w.also.Write(p)
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return len(p), nil
	}
	if w.size+int64(len(p)) > maxBytes {
		w.rotate()
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	if err != nil {
		// Stop trying. A writer that fails once per line turns one disk
		// problem into a second, louder one on every log call.
		w.file.Close()
		w.file = nil
	}
	// Report the full length regardless: the log line was delivered to the
	// destination the caller can see, and a short count would make slog
	// report an error for a line the operator is looking at.
	return len(p), nil
}

// rotate renames the live file aside and opens a fresh one. Called with the
// mutex held. Any failure leaves w.file nil rather than writing into a file
// whose name no longer means what it says.
func (w *writer) rotate() {
	w.file.Close()
	w.file = nil
	stamp := time.Now().UTC().Format("20060102-150405")
	rotated := filepath.Join(w.dir, "companion-"+stamp+".log")
	// A second rotation inside the same second must not overwrite the first.
	for i := 1; ; i++ {
		if _, err := os.Stat(rotated); os.IsNotExist(err) {
			break
		}
		rotated = filepath.Join(w.dir, fmt.Sprintf("companion-%s-%d.log", stamp, i))
	}
	if err := os.Rename(w.path, rotated); err != nil {
		return
	}
	prune(w.dir)
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	w.file = f
	w.size = 0
}

// Close releases the file. The writer stays usable and keeps feeding the
// other destination, so a log call during shutdown is not a panic.
func (w *writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

// prune keeps only the newest `keep` rotated logs.
func prune(dir string) {
	entries, err := filepath.Glob(filepath.Join(dir, "companion-*.log"))
	if err != nil {
		return
	}
	sort.Strings(entries) // timestamp names sort chronologically
	for len(entries) > keep {
		os.Remove(entries[0])
		entries = entries[1:]
	}
}
