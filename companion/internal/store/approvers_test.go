package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestApproverDevicesRoundTrip(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)

	d := &ApproverDevice{ID: "apr_1", Name: "phone", SignPub: []byte("s1"), BoxPub: []byte("b1"),
		CreatedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour)}
	if err := st.PutApprover(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := st.PutApprover(ctx, d); err == nil {
		t.Fatal("a second pairing under the same id must not silently replace the first")
	}
	got, err := st.GetApprover(ctx, "apr_1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "phone" || string(got.SignPub) != "s1" || string(got.BoxPub) != "b1" ||
		!got.CreatedAt.Equal(d.CreatedAt) || !got.ExpiresAt.Equal(d.ExpiresAt) {
		t.Fatalf("round trip lost a field: %+v", got)
	}

	later := &ApproverDevice{ID: "apr_2", Name: "laptop", SignPub: []byte("s2"), BoxPub: []byte("b2"),
		CreatedAt: now.Add(time.Hour), ExpiresAt: now.Add(31 * 24 * time.Hour)}
	if err := st.PutApprover(ctx, later); err != nil {
		t.Fatal(err)
	}
	list, err := st.ListApprovers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != "apr_2" || list[1].ID != "apr_1" {
		t.Fatalf("expected newest first, got %+v", list)
	}

	if err := st.DeleteApprover(ctx, "apr_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetApprover(ctx, "apr_1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked device still readable: %v", err)
	}
	if err := st.DeleteApprover(ctx, "apr_1"); err != nil {
		t.Fatalf("revoking twice is not an error: %v", err)
	}
}

func TestApproverDeviceRequiresBothKeys(t *testing.T) {
	st := openTestStore(t)
	now := time.Now()
	bad := &ApproverDevice{ID: "apr_x", Name: "phone", SignPub: []byte("s"), CreatedAt: now, ExpiresAt: now}
	if err := st.PutApprover(context.Background(), bad); err == nil {
		t.Fatal("a device without an encryption key could never receive a prompt; it must not be stored")
	}
}
