-- Approver devices (Batch V, V-T1/D43, 2026-09-19).
--
-- An approver is a phone or another computer the user paired from the
-- desktop so approvals can be answered where the user is, not where the
-- Companion runs. A row holds the two public keys the device presented when
-- it was paired: sign_pub (Ed25519) proves an answer came from that device,
-- box_pub (X25519) is what each prompt is encrypted to before it leaves this
-- process. Neither is a secret; the private halves never exist here.
--
-- Pairings expire on their own (30 days) because the phone's key lives in
-- browser storage rather than an OS keychain, and a bound is the cheapest
-- compensation for that. Deleting the row is the revoke, effective at once.
CREATE TABLE approver_devices (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    sign_pub   BLOB NOT NULL,
    box_pub    BLOB NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
