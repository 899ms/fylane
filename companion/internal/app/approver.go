package app

import (
	"github.com/leazoot/fylane/companion/internal/approval"
	"github.com/leazoot/fylane/companion/internal/approver"
	"github.com/leazoot/fylane/companion/internal/ctlapi"
	"github.com/leazoot/fylane/companion/internal/devicecred"
	"github.com/leazoot/fylane/companion/internal/directsrv"
	"github.com/leazoot/fylane/companion/internal/store"
	"github.com/leazoot/fylane/companion/internal/webpush"
)

// approverDevices builds the approver surface for direct mode: prompts and
// decisions come from the same approval service the desktop uses, the view
// is the desktop's own, and both keys live in the OS keychain. Without a
// push key the surface still works; the phone just has to be open.
func (a *App) approverDevices(st *store.Store, approvals *approval.Service, direct *directsrv.Server) (*approver.Service, error) {
	signer, err := devicecred.ApproverSigningKey()
	if err != nil {
		return nil, err
	}
	opts := approver.Options{
		Store:     st,
		Pending:   approvals.Pending,
		Resolve:   approvals.Resolve,
		View:      func(p *approval.Pending) any { return ctlapi.ApprovalView(p) },
		Signer:    signer,
		PublicURL: direct.PublicURL,
		Log:       a.log,
	}
	if key, err := devicecred.ApproverPushKey(); err != nil {
		a.log.Warn("approver push unavailable", "error", err)
	} else if sender, err := webpush.NewSender(key); err != nil {
		a.log.Warn("approver push unavailable", "error", err)
	} else {
		opts.Push = sender
	}
	return approver.New(opts)
}
