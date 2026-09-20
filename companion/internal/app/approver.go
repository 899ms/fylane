package app

import (
	"github.com/leazoot/fylane/companion/internal/approval"
	"github.com/leazoot/fylane/companion/internal/approver"
	"github.com/leazoot/fylane/companion/internal/ctlapi"
	"github.com/leazoot/fylane/companion/internal/devicecred"
	"github.com/leazoot/fylane/companion/internal/directsrv"
	"github.com/leazoot/fylane/companion/internal/store"
)

// approverDevices builds the approver surface for direct mode: prompts and
// decisions come from the same approval service the desktop uses, the view
// is the desktop's own, and the signing key lives in the OS keychain.
func (a *App) approverDevices(st *store.Store, approvals *approval.Service, direct *directsrv.Server) (*approver.Service, error) {
	signer, err := devicecred.ApproverSigningKey()
	if err != nil {
		return nil, err
	}
	return approver.New(approver.Options{
		Store:     st,
		Pending:   approvals.Pending,
		Resolve:   approvals.Resolve,
		View:      func(p *approval.Pending) any { return ctlapi.ApprovalView(p) },
		Signer:    signer,
		PublicURL: direct.PublicURL,
		Log:       a.log,
	})
}
