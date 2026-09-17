package pagesnap

import (
	"sort"
	"sync"
	"time"
)

// Grant is one workspace whose pages the local user let page_snapshot send.
type Grant struct {
	WorkspaceID string    `json:"workspace_id"`
	GrantedAt   time.Time `json:"granted_at"`
}

// GrantStore keeps the grants across restarts. Nil keeps them in memory,
// which is what tests and a Companion without a data directory want.
type GrantStore interface {
	Load() ([]Grant, error)
	Save([]Grant) error
}

// Grants remembers the yes per workspace. One question per folder, asked
// once: the picture goes to the platform either way, and asking again every
// restart trains the user to click through it without reading.
type Grants struct {
	store GrantStore
	mu    sync.Mutex
	m     map[string]time.Time
}

// NewGrants returns grants that live only as long as this process.
func NewGrants() *Grants { return &Grants{m: map[string]time.Time{}} }

// LoadGrants returns the grants the store holds, and keeps writing to it.
func LoadGrants(store GrantStore) (*Grants, error) {
	g := &Grants{store: store, m: map[string]time.Time{}}
	if store == nil {
		return g, nil
	}
	held, err := store.Load()
	if err != nil {
		return nil, err
	}
	for _, one := range held {
		if one.WorkspaceID != "" {
			g.m[one.WorkspaceID] = one.GrantedAt
		}
	}
	return g, nil
}

func (g *Grants) Granted(workspaceID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.m[workspaceID]
	return ok
}

// Grant records the yes and writes it down. A grant that could not be
// written is reported rather than kept quietly for this session only: the
// user was told it would not be asked again.
func (g *Grants) Grant(workspaceID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.m[workspaceID]; ok {
		return nil
	}
	g.m[workspaceID] = time.Now()
	if err := g.persistLocked(); err != nil {
		delete(g.m, workspaceID)
		return err
	}
	return nil
}

// Revoke withdraws the yes; it reports whether there was one.
func (g *Grants) Revoke(workspaceID string) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	at, ok := g.m[workspaceID]
	if !ok {
		return false, nil
	}
	delete(g.m, workspaceID)
	if err := g.persistLocked(); err != nil {
		g.m[workspaceID] = at
		return false, err
	}
	return true, nil
}

// List returns the grants, oldest first.
func (g *Grants) List() []Grant {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.listLocked()
}

func (g *Grants) listLocked() []Grant {
	out := make([]Grant, 0, len(g.m))
	for ws, at := range g.m {
		out = append(out, Grant{WorkspaceID: ws, GrantedAt: at})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GrantedAt.Before(out[j].GrantedAt) })
	return out
}

func (g *Grants) persistLocked() error {
	if g.store == nil {
		return nil
	}
	return g.store.Save(g.listLocked())
}

// Service is what page_snapshot is given: a browser, the grants, and whether
// the user's rung wants to be asked every time.
type Service struct {
	*Snapshotter
	*Grants
	// Strict reports whether the command rung asks before everything.
	Strict func() bool
}

// AskEveryTime reports whether every snapshot asks, whatever was granted.
func (s *Service) AskEveryTime() bool { return s.Strict != nil && s.Strict() }
