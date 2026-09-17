package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// A revocation knows one thing — which platform — and must not pay for that
// with the rest of the row. UpsertConnector replaces capabilities and
// token_reference from whatever the caller carries, so reusing it here would
// blank both every time a family is revoked.
func TestMarkConnectorRevokedWritesNothingButTheStatus(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "fylane.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	paired := time.Date(2026, 9, 12, 9, 0, 0, 0, time.UTC)
	called := time.Date(2026, 9, 16, 20, 57, 0, 0, time.UTC)
	before := &Connector{
		Provider:          "chatgpt",
		RemoteConnectorID: "chatgpt",
		Status:            "active",
		Capabilities:      []string{"tools", "resources"},
		LastConnectedAt:   paired,
		LastToolCallAt:    called,
		TokenReference:    "keychain:fylane/chatgpt",
	}
	if err := s.UpsertConnector(ctx, before); err != nil {
		t.Fatal(err)
	}

	if err := s.MarkConnectorRevoked(ctx, "chatgpt"); err != nil {
		t.Fatal(err)
	}

	after, err := s.GetConnector(ctx, "chatgpt", "chatgpt")
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != ConnectorRevoked {
		t.Fatalf("status = %q, want %q", after.Status, ConnectorRevoked)
	}
	if len(after.Capabilities) != 2 {
		t.Fatalf("capabilities were blanked: %v", after.Capabilities)
	}
	if after.TokenReference != before.TokenReference {
		t.Fatalf("token reference = %q, want %q", after.TokenReference, before.TokenReference)
	}
	if !after.LastConnectedAt.Equal(paired) {
		t.Fatalf("last_connected_at moved: %v", after.LastConnectedAt)
	}
	if !after.LastToolCallAt.Equal(called) {
		t.Fatalf("last_tool_call_at moved: %v", after.LastToolCallAt)
	}
}

// Revocation can arrive for a platform that authorized and never called a
// tool, so there is nothing on record to mark. That is not a failure, and
// treating it as one would turn a normal event into a logged error.
func TestMarkConnectorRevokedAcceptsAPlatformWithNoRow(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "fylane.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	if err := s.MarkConnectorRevoked(ctx, "grok"); err != nil {
		t.Fatalf("no row should not be an error: %v", err)
	}
}

// One platform's revocation must not touch another's. The column is written
// by provider, and a query that forgot its WHERE would show up here.
func TestMarkConnectorRevokedLeavesOtherPlatformsAlone(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "fylane.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	for _, p := range []string{"chatgpt", "claude"} {
		if err := s.UpsertConnector(ctx, &Connector{
			Provider: p, RemoteConnectorID: p, Status: "active",
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.MarkConnectorRevoked(ctx, "chatgpt"); err != nil {
		t.Fatal(err)
	}

	other, err := s.GetConnector(ctx, "claude", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if other.Status != "active" {
		t.Fatalf("claude status = %q, want it untouched", other.Status)
	}
}

func TestMarkConnectorRevokedRefusesAnEmptyProvider(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "fylane.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	if err := s.MarkConnectorRevoked(ctx, "   "); err == nil {
		t.Fatal("an unnamed provider was accepted")
	}
}
