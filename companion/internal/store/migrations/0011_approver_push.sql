-- Approver push subscriptions (Batch V, V-T5/D43 ⑤ revisited, 2026-09-20).
--
-- A paired phone that installed the page as a home-screen app can hand
-- over a Web Push subscription: the address of its browser's push service
-- plus the two values that service's client encryption needs (the phone's
-- P-256 public key and a 16-byte auth secret). With those the Companion can
-- wake the phone when a prompt arrives. What it sends is a fixed marker
-- ("something is waiting") — never the prompt, so nothing the push service
-- stores says what was asked.
--
-- All three are NULL until the phone opts in, and cleared again when the
-- push service reports the subscription gone. They live on the device row
-- because a subscription is one per device and dies with it.
ALTER TABLE approver_devices ADD COLUMN push_endpoint TEXT;
ALTER TABLE approver_devices ADD COLUMN push_p256dh BLOB;
ALTER TABLE approver_devices ADD COLUMN push_auth BLOB;
