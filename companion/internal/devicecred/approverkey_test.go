package devicecred

import (
	"bytes"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestApproverSigningKeyIsMadeOnceAndKept(t *testing.T) {
	keyring.MockInit()
	first, err := ApproverSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	second, err := ApproverSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("a second call minted a new key; every paired device would stop trusting this Companion")
	}
	keyring.MockInit()
	third, err := ApproverSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, third) {
		t.Fatal("a fresh keychain must get a fresh key")
	}
}

func TestACorruptStoredKeyIsRefusedNotReplaced(t *testing.T) {
	keyring.MockInit()
	if err := keyring.Set(keyringService, approverKeyEntry, "not-a-seed"); err != nil {
		t.Fatal(err)
	}
	if _, err := ApproverSigningKey(); err == nil {
		t.Fatal("garbage in the keychain must be reported, not silently overwritten")
	}
}
