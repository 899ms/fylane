package devicecred

import (
	"testing"

	"github.com/zalando/go-keyring"
)

func TestThePushKeyIsMadeOnceAndKept(t *testing.T) {
	keyring.MockInit()
	first, err := ApproverPushKey()
	if err != nil {
		t.Fatal(err)
	}
	second, err := ApproverPushKey()
	if err != nil {
		t.Fatal(err)
	}
	if !first.Equal(second) {
		t.Fatal("a second call must return the stored key, not a new one")
	}
}

func TestACorruptStoredPushKeyIsAnErrorNotAFreshKey(t *testing.T) {
	keyring.MockInit()
	if err := keyring.Set(keyringService, approverPushEntry, "not-a-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := ApproverPushKey(); err == nil {
		t.Fatal("a corrupt entry must not be silently replaced: the phones subscribed with the old public key")
	}
}
