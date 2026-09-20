package devicecred

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/zalando/go-keyring"
)

// approverKeyEntry names the keychain entry holding the seed of the key this
// Companion signs approver envelopes with (D43). One per machine, made on
// first use; the paired devices hold its public half.
const approverKeyEntry = "approver:signing"

// ApproverSigningKey returns this Companion's approver signing key, creating
// and storing it in the OS keychain the first time. The seed never touches a
// config file or the database, for the same reason relay credentials do not.
func ApproverSigningKey() (ed25519.PrivateKey, error) {
	raw, err := keyring.Get(keyringService, approverKeyEntry)
	if err == nil {
		seed, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || len(seed) != ed25519.SeedSize {
			return nil, fmt.Errorf("stored approver signing key is invalid")
		}
		return ed25519.NewKeyFromSeed(seed), nil
	}
	if err != keyring.ErrNotFound {
		return nil, fmt.Errorf("reading the approver signing key from the OS keychain: %w", err)
	}
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, fmt.Errorf("generating the approver signing key: %w", err)
	}
	if err := keyring.Set(keyringService, approverKeyEntry, base64.RawURLEncoding.EncodeToString(seed)); err != nil {
		return nil, fmt.Errorf("storing the approver signing key in the OS keychain: %w", err)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}
