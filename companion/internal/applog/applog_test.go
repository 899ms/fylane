package applog

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestTeeReachesBothDestinations(t *testing.T) {
	dir := t.TempDir()
	var also bytes.Buffer
	w, closeLog, err := OpenTo(dir, &also)
	if err != nil {
		t.Fatal(err)
	}
	defer closeLog()

	if _, err := w.Write([]byte("a line\n")); err != nil {
		t.Fatal(err)
	}
	if got := also.String(); got != "a line\n" {
		t.Fatalf("second destination = %q", got)
	}
	body, err := os.ReadFile(filepath.Join(dir, DirName, latestName))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "a line\n" {
		t.Fatalf("file = %q", body)
	}
}

func TestRotatesPastTheCap(t *testing.T) {
	dir := t.TempDir()
	w, closeLog, err := OpenTo(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeLog()

	// Fill it to the cap exactly — that must not rotate, or a file one byte
	// short of the limit would be rolled for nothing.
	if _, err := w.Write(bytes.Repeat([]byte("x"), maxBytes)); err != nil {
		t.Fatal(err)
	}
	logs := filepath.Join(dir, DirName)
	if rotated, _ := filepath.Glob(filepath.Join(logs, "companion-*.log")); len(rotated) != 0 {
		t.Fatalf("rotated at the cap, before passing it: %v", rotated)
	}

	if _, err := w.Write([]byte("the line that tips it\n")); err != nil {
		t.Fatal(err)
	}
	rotated, _ := filepath.Glob(filepath.Join(logs, "companion-*.log"))
	if len(rotated) != 1 {
		t.Fatalf("rotated files = %v", rotated)
	}
	if info, err := os.Stat(rotated[0]); err != nil || info.Size() != maxBytes {
		t.Fatalf("rotated file holds what was there: %v %v", info, err)
	}
	// The live file starts again from the line that caused the roll — it is
	// written, not dropped, which is the whole point of rotating first.
	body, err := os.ReadFile(filepath.Join(logs, latestName))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "the line that tips it\n" {
		t.Fatalf("live file after rotation = %q", body)
	}
}

func TestPruneKeepsOnlyTheNewest(t *testing.T) {
	logs := filepath.Join(t.TempDir(), DirName)
	if err := os.MkdirAll(logs, 0o700); err != nil {
		t.Fatal(err)
	}
	// Names carry the timestamp, so lexical order is chronological order.
	var made []string
	for i := 1; i <= keep+3; i++ {
		name := fmt.Sprintf("companion-2026091%d-000000.log", i)
		if err := os.WriteFile(filepath.Join(logs, name), []byte("old\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		made = append(made, name)
	}
	if err := os.WriteFile(filepath.Join(logs, latestName), []byte("live\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	prune(logs)

	left, _ := filepath.Glob(filepath.Join(logs, "companion-*.log"))
	if len(left) != keep {
		t.Fatalf("kept %d rotated files, want %d: %v", len(left), keep, left)
	}
	// The ones that survived must be the newest, not just any five.
	for _, l := range left {
		if base := filepath.Base(l); base < made[3] {
			t.Fatalf("kept an older file than it should have: %s", base)
		}
	}
	// The live file is not a rotation and must never be pruned.
	if _, err := os.Stat(filepath.Join(logs, latestName)); err != nil {
		t.Fatalf("live log was pruned: %v", err)
	}
}

func TestConcurrentWritesKeepEveryLine(t *testing.T) {
	dir := t.TempDir()
	w, closeLog, err := OpenTo(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeLog()

	// slog writes from whichever goroutine logged. Without the mutex two
	// writes interleave and produce a line that never happened.
	const writers, each = 8, 200
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < each; j++ {
				fmt.Fprintf(w, "writer=%d line=%d\n", n, j)
			}
		}(i)
	}
	wg.Wait()

	body, err := os.ReadFile(filepath.Join(dir, DirName, latestName))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
	if len(lines) != writers*each {
		t.Fatalf("lines = %d, want %d", len(lines), writers*each)
	}
	for _, l := range lines {
		if !strings.HasPrefix(l, "writer=") || !strings.Contains(l, " line=") {
			t.Fatalf("torn line: %q", l)
		}
	}
}

func TestOpenReportsWhatItCannotDo(t *testing.T) {
	// A file where the directory has to go: the caller has to hear about it
	// and fall back to stderr rather than start a Core with no log at all.
	blocked := filepath.Join(t.TempDir(), "in-the-way")
	if err := os.WriteFile(blocked, []byte("not a directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenTo(blocked, nil); err == nil {
		t.Fatal("opening under a plain file succeeded")
	}
}

func TestWriteSurvivesACloseMidRun(t *testing.T) {
	dir := t.TempDir()
	var also bytes.Buffer
	w, closeLog, err := OpenTo(dir, &also)
	if err != nil {
		t.Fatal(err)
	}
	if err := closeLog(); err != nil {
		t.Fatal(err)
	}
	// Shutdown logs a few lines after the file is closed. They still have to
	// reach the operator, and they must not panic.
	if _, err := w.Write([]byte("after close\n")); err != nil {
		t.Fatalf("write after close: %v", err)
	}
	if !strings.Contains(also.String(), "after close") {
		t.Fatalf("second destination lost the line: %q", also.String())
	}
}
