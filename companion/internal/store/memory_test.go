package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

var testTime = time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC)

func TestMemoryPageIsRewrittenAndBounded(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if err := s.CreateWorkspace(ctx, testWorkspace("ws-m")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetMemoryState(ctx, "ws-m"); err != ErrNotFound {
		t.Fatalf("fresh workspace: %v", err)
	}
	page := MemoryPage{Goal: "ship v1", Progress: "auth done", Next: "billing", Decisions: []string{"sqlite"}}
	if err := s.SaveMemoryState(ctx, "ws-m", "claude", page, testTime); err != nil {
		t.Fatal(err)
	}
	page.Progress = "auth and billing done"
	page.Decisions = nil
	if err := s.SaveMemoryState(ctx, "ws-m", "chatgpt", page, testTime); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMemoryState(ctx, "ws-m")
	if err != nil {
		t.Fatal(err)
	}
	if got.Page.Progress != "auth and billing done" || len(got.Page.Decisions) != 0 || got.Provider != "chatgpt" || !got.UpdatedAt.Equal(testTime) {
		t.Fatalf("page after rewrite = %+v", got)
	}
	for _, bad := range []MemoryPage{
		{Goal: strings.Repeat("g", MemoryGoalBytes+1)},
		{Decisions: make([]string, MemoryListItems+1)},
		{Open: []string{strings.Repeat("o", MemoryListItemBytes+1)}},
		{Next: "\xff"},
	} {
		if err := s.SaveMemoryState(ctx, "ws-m", "", bad, testTime); err == nil {
			t.Errorf("page %+v was accepted", bad)
		}
	}
}

func TestMemoryNotesArePagedSearchedAndScopedToTheirWorkspace(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"ws-a", "ws-b"} {
		if err := s.CreateWorkspace(ctx, testWorkspace(id)); err != nil {
			t.Fatal(err)
		}
	}
	for i, title := range []string{"login form", "billing page", "login bug fixed", "100% coverage"} {
		n := &MemoryNote{WorkspaceID: "ws-a", Provider: "claude", Title: title, Body: "body " + title}
		if i == 1 {
			n.WorkspaceID = "ws-b"
		}
		if err := s.AddMemoryNote(ctx, n); err != nil {
			t.Fatal(err)
		}
		if n.ID == 0 || n.CreatedAt.IsZero() {
			t.Fatalf("note %q: id %d created %s", title, n.ID, n.CreatedAt)
		}
	}
	page, err := s.ListMemoryNotes(ctx, "ws-a", 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || page[0].Title != "100% coverage" || page[1].Title != "login bug fixed" {
		t.Fatalf("first page = %v", titles(page))
	}
	rest, _ := s.ListMemoryNotes(ctx, "ws-a", page[1].ID, 10)
	if len(rest) != 1 || rest[0].Title != "login form" {
		t.Fatalf("second page = %v", titles(rest))
	}
	found, err := s.SearchMemoryNotes(ctx, "ws-a", "LOGIN bug", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Title != "login bug fixed" {
		t.Fatalf("search = %v", titles(found))
	}
	if found, _ := s.SearchMemoryNotes(ctx, "ws-a", "100%", 10); len(found) != 1 {
		t.Fatalf("a literal %% was read as a wildcard: %v", titles(found))
	}
	if found, _ := s.SearchMemoryNotes(ctx, "ws-a", "billing", 10); len(found) != 0 {
		t.Fatalf("a note of another workspace was found: %v", titles(found))
	}
	other, _ := s.ListMemoryNotes(ctx, "ws-b", 0, 10)
	if got, _ := s.GetMemoryNotes(ctx, "ws-a", []int64{other[0].ID, page[0].ID}); len(got) != 1 || got[0].ID != page[0].ID {
		t.Fatalf("notes by id crossed workspaces: %v", titles(got))
	}
	if live, archived, _ := s.CountMemoryNotes(ctx, "ws-a"); live != 3 || archived != 0 {
		t.Fatalf("count = %d live, %d archived", live, archived)
	}
	if err := s.AddMemoryNote(ctx, &MemoryNote{WorkspaceID: "ws-a", Title: "x", Body: strings.Repeat("b", MemoryBodyBytes+1)}); err == nil {
		t.Fatal("an oversized body was accepted")
	}
	if err := s.DeleteMemory(ctx, "ws-a"); err != nil {
		t.Fatal(err)
	}
	if live, _, _ := s.CountMemoryNotes(ctx, "ws-a"); live != 0 {
		t.Fatal("memory survived deletion")
	}
	if other, _ := s.ListMemoryNotes(ctx, "ws-b", 0, 10); len(other) != 1 {
		t.Fatal("deleting one workspace's memory took another's")
	}
}

func titles(notes []*MemoryNote) []string {
	out := make([]string, 0, len(notes))
	for _, n := range notes {
		out = append(out, n.Title)
	}
	return out
}

func TestMemoryNotesAreArchivedInOrderAndStayFindable(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if err := s.CreateWorkspace(ctx, testWorkspace("ws-c")); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, title := range []string{"one", "two", "three"} {
		n := &MemoryNote{WorkspaceID: "ws-c", Title: title, Body: "b"}
		if err := s.AddMemoryNote(ctx, n); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n.ID)
	}
	oldest, _ := s.OldestMemoryNotes(ctx, "ws-c", 2)
	if len(oldest) != 2 || oldest[0].Title != "one" || oldest[1].Title != "two" {
		t.Fatalf("oldest = %v", titles(oldest))
	}
	if n, err := s.ArchiveMemoryNotes(ctx, "ws-c", ids[1]); err != nil || n != 2 {
		t.Fatalf("archived %d, %v", n, err)
	}
	if live, archived, _ := s.CountMemoryNotes(ctx, "ws-c"); live != 1 || archived != 2 {
		t.Fatalf("count = %d live, %d archived", live, archived)
	}
	if page, _ := s.ListMemoryNotes(ctx, "ws-c", 0, 10); len(page) != 1 || page[0].Title != "three" {
		t.Fatalf("listing shows archived notes: %v", titles(page))
	}
	if found, _ := s.SearchMemoryNotes(ctx, "ws-c", "one", 10); len(found) != 1 || !found[0].Archived {
		t.Fatalf("an archived note is not searchable: %v", titles(found))
	}
	if got, _ := s.GetMemoryNotes(ctx, "ws-c", ids[:1]); len(got) != 1 {
		t.Fatal("an archived note is not readable by id")
	}
	if n, _ := s.ArchiveMemoryNotes(ctx, "ws-c", ids[1]); n != 0 {
		t.Fatal("archiving is not idempotent")
	}
	if page, _ := s.ListArchivedMemoryNotes(ctx, "ws-c", 0, 10); len(page) != 2 || page[0].Title != "two" || page[1].Title != "one" {
		t.Fatalf("archived listing = %v", titles(page))
	}
	if page, _ := s.ListArchivedMemoryNotes(ctx, "ws-c", ids[1], 10); len(page) != 1 || page[0].Title != "one" {
		t.Fatalf("archived listing past the first = %v", titles(page))
	}
	// A note is deleted within its workspace and only once.
	if err := s.DeleteMemoryNote(ctx, "ws-other", ids[2]); err != ErrNotFound {
		t.Fatalf("deleting through another workspace: %v", err)
	}
	if err := s.DeleteMemoryNote(ctx, "ws-c", ids[2]); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteMemoryNote(ctx, "ws-c", ids[2]); err != ErrNotFound {
		t.Fatalf("deleting twice: %v", err)
	}
	if live, archived, _ := s.CountMemoryNotes(ctx, "ws-c"); live != 0 || archived != 2 {
		t.Fatalf("count after delete = %d live, %d archived", live, archived)
	}
}

func TestMemoryPlanIsReplacedWholeAndMovedOneStepAtATime(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if err := s.CreateWorkspace(ctx, testWorkspace("ws-p")); err != nil {
		t.Fatal(err)
	}
	if plan, err := s.ListMemoryPlan(ctx, "ws-p"); err != nil || len(plan) != 0 {
		t.Fatalf("fresh workspace: %+v %v", plan, err)
	}

	saved, err := s.SaveMemoryPlan(ctx, "ws-p", "claude", []MemoryStep{
		{Title: "read the middleware"},
		{Title: "move the refresh", State: StepDoing},
	}, testTime)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 2 || saved[0].Position != 1 || saved[0].State != StepTodo || saved[1].State != StepDoing {
		t.Fatalf("saved plan = %+v %+v", saved[0], saved[1])
	}

	// Moving one step touches that row only, and records who moved it — a
	// plan carried between platforms has to say where each step was moved.
	note := "the fixture server has no refresh endpoint"
	if err := s.UpdateMemoryStep(ctx, "ws-p", "chatgpt", saved[1].ID, StepBlocked, nil, &note, testTime); err != nil {
		t.Fatal(err)
	}
	plan, err := s.ListMemoryPlan(ctx, "ws-p")
	if err != nil {
		t.Fatal(err)
	}
	if plan[1].State != StepBlocked || plan[1].Note != note || plan[1].Provider != "chatgpt" {
		t.Fatalf("moved step = %+v", plan[1])
	}
	if plan[0].State != StepTodo || plan[0].Provider != "claude" {
		t.Fatalf("the other step was touched: %+v", plan[0])
	}

	// A nil note leaves the note where it is.
	if err := s.UpdateMemoryStep(ctx, "ws-p", "claude", plan[1].ID, StepDone, nil, nil, testTime); err != nil {
		t.Fatal(err)
	}
	if plan, _ = s.ListMemoryPlan(ctx, "ws-p"); plan[1].State != StepDone || plan[1].Note != note {
		t.Fatalf("note after a move that did not mention it = %+v", plan[1])
	}

	// Rewriting replaces the plan whole, so the old ids stop resolving: an
	// update aimed at a plan nobody is working on must fail, not land.
	oldID := plan[0].ID
	if _, err := s.SaveMemoryPlan(ctx, "ws-p", "claude", []MemoryStep{{Title: "start over"}}, testTime); err != nil {
		t.Fatal(err)
	}
	fresh, _ := s.ListMemoryPlan(ctx, "ws-p")
	if len(fresh) != 1 || fresh[0].Title != "start over" {
		t.Fatalf("rewritten plan = %+v", fresh)
	}
	if err := s.UpdateMemoryStep(ctx, "ws-p", "claude", oldID, StepDone, nil, nil, testTime); err != ErrNotFound {
		t.Fatalf("update against a replaced plan = %v, want ErrNotFound", err)
	}

	// Bounds and the closed state set are refused here too, whatever the
	// tool layer did or did not cut on the way in.
	for _, bad := range [][]MemoryStep{
		{{Title: "  "}},
		{{Title: strings.Repeat("t", MemoryStepTitleBytes+1)}},
		{{Title: "ok", Note: strings.Repeat("n", MemoryStepNoteBytes+1)}},
		{{Title: "ok", State: "almost"}},
		make([]MemoryStep, MemoryPlanSteps+1),
	} {
		if _, err := s.SaveMemoryPlan(ctx, "ws-p", "", bad, testTime); err == nil {
			t.Errorf("plan %+v was accepted", bad)
		}
	}
	if err := s.UpdateMemoryStep(ctx, "ws-p", "", fresh[0].ID, "almost", nil, nil, testTime); err == nil {
		t.Error("an invented state was accepted")
	}

	// Forgetting a workspace forgets its plan too, or "forget everything"
	// would be a promise the plan outlives.
	if err := s.DeleteMemory(ctx, "ws-p"); err != nil {
		t.Fatal(err)
	}
	if plan, _ := s.ListMemoryPlan(ctx, "ws-p"); len(plan) != 0 {
		t.Fatalf("the plan survived DeleteMemory: %+v", plan)
	}
}
