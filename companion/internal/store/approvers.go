package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ApproverDevice is a paired device allowed to answer approvals for this
// Companion. Only public keys are stored; see migration 0010.
type ApproverDevice struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	SignPub   []byte    `json:"-"`
	BoxPub    []byte    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	// The device's Web Push subscription, all empty until it opts in; see
	// migration 0011. The endpoint is an address of the phone's push
	// service, not of the phone, and says nothing about the user.
	PushEndpoint string `json:"-"`
	PushP256DH   []byte `json:"-"`
	PushAuth     []byte `json:"-"`
}

// SetApproverPush records, or with an empty endpoint clears, the device's
// push subscription. A device that is not there is ErrNotFound: a
// subscription cannot outlive the pairing it belongs to.
func (s *Store) SetApproverPush(ctx context.Context, id, endpoint string, p256dh, auth []byte) error {
	if endpoint == "" {
		p256dh, auth = nil, nil
	} else if len(p256dh) == 0 || len(auth) == 0 {
		return fmt.Errorf("approver push: endpoint, p256dh and auth are required together")
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE approver_devices SET push_endpoint = ?, push_p256dh = ?, push_auth = ? WHERE id = ?`,
		nullString(endpoint), p256dh, auth, id)
	if err != nil {
		return fmt.Errorf("storing approver push subscription: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func nullString(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

// PutApprover records a newly paired device. Pairing is the only way a row is
// created, so an existing id is a programming error rather than an update.
func (s *Store) PutApprover(ctx context.Context, d *ApproverDevice) error {
	if d.ID == "" || d.Name == "" || len(d.SignPub) == 0 || len(d.BoxPub) == 0 {
		return fmt.Errorf("approver device: id, name and both public keys are required")
	}
	if d.CreatedAt.IsZero() || d.ExpiresAt.IsZero() {
		return fmt.Errorf("approver device: created_at and expires_at are required")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO approver_devices (id, name, sign_pub, box_pub, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		d.ID, d.Name, d.SignPub, d.BoxPub, formatTime(d.CreatedAt), formatTime(d.ExpiresAt))
	if err != nil {
		return fmt.Errorf("storing approver device: %w", err)
	}
	return nil
}

// GetApprover returns one device, or ErrNotFound. Expiry is the caller's
// judgement: the row says when, the caller says whether that is past.
func (s *Store) GetApprover(ctx context.Context, id string) (*ApproverDevice, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, sign_pub, box_pub, created_at, expires_at, push_endpoint, push_p256dh, push_auth
		FROM approver_devices WHERE id = ?`, id)
	d, err := scanApprover(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return d, err
}

// ListApprovers returns every paired device, newest first, expired ones
// included: a pairing that lapsed still belongs on the settings page until
// the user removes it or pairs again.
func (s *Store) ListApprovers(ctx context.Context) ([]*ApproverDevice, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, sign_pub, box_pub, created_at, expires_at, push_endpoint, push_p256dh, push_auth
		FROM approver_devices ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, fmt.Errorf("listing approver devices: %w", err)
	}
	defer rows.Close()
	var out []*ApproverDevice
	for rows.Next() {
		d, err := scanApprover(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DeleteApprover revokes a device. Deleting one that is not there is not an
// error: the caller wanted it gone, and it is.
func (s *Store) DeleteApprover(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM approver_devices WHERE id = ?`, id); err != nil {
		return fmt.Errorf("revoking approver device: %w", err)
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanApprover(row scanner) (*ApproverDevice, error) {
	var (
		d                ApproverDevice
		created, expires string
		endpoint         sql.NullString
	)
	if err := row.Scan(&d.ID, &d.Name, &d.SignPub, &d.BoxPub, &created, &expires, &endpoint, &d.PushP256DH, &d.PushAuth); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scanning approver device: %w", err)
	}
	d.PushEndpoint = endpoint.String
	var err error
	if d.CreatedAt, err = parseTime(created); err != nil {
		return nil, fmt.Errorf("decoding approver timestamp: %w", err)
	}
	if d.ExpiresAt, err = parseTime(expires); err != nil {
		return nil, fmt.Errorf("decoding approver timestamp: %w", err)
	}
	return &d, nil
}
