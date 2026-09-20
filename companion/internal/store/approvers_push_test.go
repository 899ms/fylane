package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAPushSubscriptionIsKeptOnTheDeviceAndCanBeCleared(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	d := &ApproverDevice{ID: "apr_1", Name: "phone", SignPub: []byte("s1"), BoxPub: []byte("b1"),
		CreatedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour)}
	if err := st.PutApprover(ctx, d); err != nil {
		t.Fatal(err)
	}
	// A freshly paired device has no subscription, and the columns being
	// NULL must read as empty, not fail.
	got, err := st.GetApprover(ctx, "apr_1")
	if err != nil {
		t.Fatal(err)
	}
	if got.PushEndpoint != "" || got.PushP256DH != nil || got.PushAuth != nil {
		t.Fatalf("new device must have no subscription: %+v", got)
	}

	if err := st.SetApproverPush(ctx, "apr_1", "https://push.example.net/s/1", []byte("p"), []byte("a")); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetApprover(ctx, "apr_1")
	if got.PushEndpoint != "https://push.example.net/s/1" || string(got.PushP256DH) != "p" || string(got.PushAuth) != "a" {
		t.Fatalf("subscription not stored: %+v", got)
	}
	list, _ := st.ListApprovers(ctx)
	if len(list) != 1 || list[0].PushEndpoint == "" {
		t.Fatal("the list must carry the subscription too: that is what a notify reads")
	}

	if err := st.SetApproverPush(ctx, "apr_1", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetApprover(ctx, "apr_1")
	if got.PushEndpoint != "" || got.PushP256DH != nil || got.PushAuth != nil {
		t.Fatalf("clearing must leave nothing behind: %+v", got)
	}

	if err := st.SetApproverPush(ctx, "apr_missing", "https://push.example.net/s/2", []byte("p"), []byte("a")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a subscription for a device that is not there must be ErrNotFound, got %v", err)
	}
	if err := st.SetApproverPush(ctx, "apr_1", "https://push.example.net/s/1", nil, []byte("a")); err == nil {
		t.Fatal("an endpoint without both keys is not a subscription")
	}
}
