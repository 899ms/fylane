package devicecred

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"fmt"

	"github.com/zalando/go-keyring"
)

// approverPushEntry names the keychain entry holding the VAPID key this
// Companion signs push messages with (V-T5). One per machine, made on first
// use; a phone subscribes with its public half, and a push service will not
// take a message signed by any other key for that subscription.
const approverPushEntry = "approver:push"

// ApproverPushKey returns this Companion's Web Push signing key, creating and
// storing it in the OS keychain the first time. Like the envelope signing
// key, it never touches a config file or the database.
func ApproverPushKey() (*ecdsa.PrivateKey, error) {
	raw, err := keyring.Get(keyringService, approverPushEntry)
	if err == nil {
		der, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil {
			return nil, fmt.Errorf("stored approver push key is invalid")
		}
		parsed, err := x509.ParsePKCS8PrivateKey(der)
		key, ok := parsed.(*ecdsa.PrivateKey)
		if err != nil || !ok || key.Curve != elliptic.P256() {
			return nil, fmt.Errorf("stored approver push key is invalid")
		}
		return key, nil
	}
	if err != keyring.ErrNotFound {
		return nil, fmt.Errorf("reading the approver push key from the OS keychain: %w", err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating the approver push key: %w", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("encoding the approver push key: %w", err)
	}
	if err := keyring.Set(keyringService, approverPushEntry, base64.RawURLEncoding.EncodeToString(der)); err != nil {
		return nil, fmt.Errorf("storing the approver push key in the OS keychain: %w", err)
	}
	return key, nil
}
