// Package tenants is a copyable pattern for one SQLite database per tenant on
// modernc.org/sqlite: <root>/<id>.db, a single writer per tenant, a bounded
// LRU cache of open handles, per-tenant migrations, and online backups.
//
// One process owns the root directory. Read README.md before copying.
package tenants

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
)

var (
	ErrNotFound  = errors.New("tenants: tenant not found")
	ErrExists    = errors.New("tenants: tenant already exists")
	ErrInvalidID = errors.New("tenants: invalid tenant id")
	ErrClosed    = errors.New("tenants: manager closed")
)

// MigrationError means a tenant could not be brought to the current schema.
// The tenant stays at Version and is not served until a later open succeeds.
type MigrationError struct {
	Tenant  string
	Version int // user_version the tenant is still at
	Err     error
}

func (e *MigrationError) Error() string {
	return fmt.Sprintf("tenants: migrate %s from version %d: %v", e.Tenant, e.Version, e.Err)
}

func (e *MigrationError) Unwrap() error { return e.Err }

// The ID is the file name, so the pattern is the path-traversal defence.
var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

type Config struct {
	Root       string   // directory of <id>.db files; created if missing
	MaxOpen    int      // most tenants held open at once; default 64
	Migrations []string // ordered SQL steps; step i takes user_version i to i+1
}

// Stats is a snapshot of the handle cache.
type Stats struct {
	Open, PeakOpen          int // tenants with open handles: now, and the most ever
	Hits, Misses, Evictions uint64
}

type Result struct {
	ID  string
	Err error
}

type Manager struct {
	root    string
	maxOpen int
	steps   []string

	mu      sync.Mutex
	tenants map[string]*Tenant // opening, open, or being deleted
	slots   int                // tenants holding or about to hold handles; <= maxOpen
	idle    *list.List         // open tenants nobody is using, most recent first
	changed chan struct{}      // closed and replaced when waiters should look again
	waiters int
	closed  bool
	stats   Stats
}

func New(cfg Config) (*Manager, error) {
	root, err := filepath.Abs(cfg.Root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	if cfg.MaxOpen <= 0 {
		cfg.MaxOpen = 64
	}
	return &Manager{
		root: root, maxOpen: cfg.MaxOpen, steps: cfg.Migrations,
		tenants: map[string]*Tenant{}, idle: list.New(), changed: make(chan struct{}),
	}, nil
}

// Create makes a new tenant at the current schema version. It fails with
// ErrExists if the tenant is already there.
func (m *Manager) Create(ctx context.Context, id string) error {
	t, err := m.acquire(ctx, id, true)
	if err != nil {
		return err
	}
	m.release(t)
	return nil
}

// With runs fn with the tenant open. It never creates a tenant: an unknown ID
// is ErrNotFound. A tenant that cannot be migrated is a *MigrationError. The
// tenant stays open, and cannot be evicted or deleted, until fn returns; do
// not keep t afterwards. Calling With inside fn can deadlock a full cache.
func (m *Manager) With(ctx context.Context, id string, fn func(t *Tenant) error) error {
	t, err := m.acquire(ctx, id, false)
	if err != nil {
		return err
	}
	defer m.release(t)
	return fn(t)
}

// Delete refuses new users, waits for the ones inside With, then closes the
// handles and removes the files. If ctx ends first the tenant stays.
func (m *Manager) Delete(ctx context.Context, id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("%w: %q", ErrInvalidID, id)
	}
	m.mu.Lock()
	t := m.tenants[id]
	for ; t != nil && t.opening; t = m.tenants[id] {
		if err := m.wait(ctx); err != nil {
			m.mu.Unlock()
			return err
		}
	}
	switch {
	case m.closed:
		m.mu.Unlock()
		return ErrClosed
	case t == nil: // not open: claim the ID so nobody opens it meanwhile
		t = &Tenant{id: id, path: m.path(id)}
		m.tenants[id] = t
	case t.doomed:
		m.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	t.doomed = true
	for t.refs > 0 {
		if err := m.wait(ctx); err != nil {
			t.doomed = false
			if t.refs == 0 {
				t.idle = m.idle.PushFront(t)
			}
			m.broadcast()
			m.mu.Unlock()
			return err
		}
	}
	if t.idle != nil {
		m.idle.Remove(t.idle)
	}
	m.mu.Unlock()

	err := t.close()
	if rmErr := removeFiles(t.path); errors.Is(rmErr, fs.ErrNotExist) {
		err = errors.Join(err, fmt.Errorf("%w: %s", ErrNotFound, id))
	} else {
		err = errors.Join(err, rmErr)
	}

	m.mu.Lock()
	delete(m.tenants, id)
	if t.w != nil {
		m.slots--
		m.stats.Open--
	}
	m.broadcast()
	m.mu.Unlock()
	return err
}

// Backup writes a consistent snapshot of a live tenant to dst, which must not
// exist. The copy is an ordinary database file: restoring is putting it back
// as <root>/<id>.db.
func (m *Manager) Backup(ctx context.Context, id, dst string) error {
	return m.With(ctx, id, func(t *Tenant) error { return t.backup(ctx, dst) })
}

// IDs lists the tenants on disk, sorted.
func (m *Manager) IDs() ([]string, error) {
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if id, ok := strings.CutSuffix(e.Name(), ".db"); ok && idPattern.MatchString(id) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// ForEach runs fn against every tenant, concurrency at a time, and reports
// each tenant's outcome. One tenant failing does not stop the others. This is
// how cross-tenant reads work: ATTACH stops at 10 databases.
func (m *Manager) ForEach(ctx context.Context, concurrency int, fn func(t *Tenant) error) ([]Result, error) {
	ids, err := m.IDs()
	if err != nil {
		return nil, err
	}
	results := make([]Result, len(ids))
	var next atomic.Int64
	var wg sync.WaitGroup
	for range max(1, concurrency) {
		wg.Go(func() {
			for i := int(next.Add(1)) - 1; i < len(ids); i = int(next.Add(1)) - 1 {
				err := ctx.Err()
				if err == nil {
					err = m.With(ctx, ids[i], fn)
				}
				results[i] = Result{ID: ids[i], Err: err}
			}
		})
	}
	wg.Wait()
	return results, nil
}

// MigrateAll opens every tenant, which migrates the ones that are behind. A
// tenant whose migration fails reports a *MigrationError and keeps its old
// version; the rest carry on.
func (m *Manager) MigrateAll(ctx context.Context, concurrency int) ([]Result, error) {
	return m.ForEach(ctx, concurrency, func(*Tenant) error { return nil })
}

func (m *Manager) Stats() Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stats
}

// Close closes every open handle. Call it after all With calls have returned.
func (m *Manager) Close() error {
	m.mu.Lock()
	open := m.tenants
	m.closed, m.tenants, m.slots, m.stats.Open = true, map[string]*Tenant{}, 0, 0
	m.idle.Init()
	m.broadcast()
	m.mu.Unlock()
	var err error
	for _, t := range open {
		err = errors.Join(err, t.close())
	}
	return err
}

func (m *Manager) path(id string) string { return filepath.Join(m.root, id+".db") }

func (m *Manager) onDisk(id string) bool {
	_, err := os.Stat(m.path(id))
	return !errors.Is(err, fs.ErrNotExist)
}

// acquire returns the tenant with refs held, opening (or creating) it first
// if needed. When the cache is full it evicts the least recently used idle
// tenant; when every open tenant is in use it waits.
func (m *Manager) acquire(ctx context.Context, id string, create bool) (*Tenant, error) {
	if !idPattern.MatchString(id) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidID, id)
	}
	var victim *Tenant
	m.mu.Lock()
	for {
		t := m.tenants[id]
		switch {
		case m.closed:
			m.mu.Unlock()
			return nil, ErrClosed
		case t != nil && t.doomed && !create:
			m.mu.Unlock()
			return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
		case t != nil && !t.opening && !t.doomed && create:
			m.mu.Unlock()
			return nil, fmt.Errorf("%w: %s", ErrExists, id)
		case t != nil && !t.opening && !t.doomed:
			if t.idle != nil {
				m.idle.Remove(t.idle)
				t.idle = nil
			}
			t.refs++
			m.stats.Hits++
			m.mu.Unlock()
			return t, nil
		case t == nil && !create && !m.onDisk(id): // before claiming a slot: a typo must not evict anyone
			m.mu.Unlock()
			return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
		case t == nil && create && m.onDisk(id):
			m.mu.Unlock()
			return nil, fmt.Errorf("%w: %s", ErrExists, id)
		case t == nil && m.slots < m.maxOpen:
			m.slots++
		case t == nil && m.idle.Len() > 0:
			// The victim stays in the map, marked opening, until its handles are
			// closed: acquirers and Delete for its ID wait instead of opening the
			// same file a second time while it checkpoints.
			victim = m.idle.Remove(m.idle.Back()).(*Tenant)
			victim.idle, victim.opening = nil, true
			m.stats.Evictions++
		default: // someone else is opening or deleting it, or the cache is full of busy tenants
			if err := m.wait(ctx); err != nil {
				m.mu.Unlock()
				return nil, err
			}
			continue
		}
		break
	}
	t := &Tenant{id: id, path: m.path(id), opening: true}
	m.tenants[id] = t
	m.stats.Misses++
	m.mu.Unlock()

	// The victim's slot is ours. Close it before opening, so the number of
	// tenants with open handles never exceeds maxOpen.
	if victim != nil {
		_ = victim.close()
		m.mu.Lock()
		if m.tenants[victim.id] == victim {
			delete(m.tenants, victim.id)
		}
		m.broadcast()
		m.mu.Unlock()
		m.handles(-1)
	}
	err := m.open(ctx, t, create)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.broadcast()
	if err != nil {
		delete(m.tenants, id)
		m.slots--
		return nil, err
	}
	t.opening, t.refs = false, 1
	return t, nil
}

func (m *Manager) open(ctx context.Context, t *Tenant, create bool) error {
	if create {
		f, err := os.OpenFile(t.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%w: %s", ErrExists, t.id)
		} else if err != nil {
			return err
		}
		_ = f.Close()
	}
	m.handles(+1)
	w, r, err := openDBs(ctx, t.path)
	if err == nil {
		t.w, t.r = w, r
		if err = migrate(ctx, t.id, w, m.steps); err == nil {
			return nil
		}
		_ = t.close()
		t.w, t.r = nil, nil
	}
	m.handles(-1)
	if create {
		_ = removeFiles(t.path)
	}
	return err
}

// removeFiles deletes the sidecars first and the database last: a crash in
// between leaves a valid tenant to delete again, never a stale WAL waiting
// for the next tenant with this ID.
func removeFiles(path string) error {
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return os.Remove(path)
}

// handles counts tenants with open handles. It goes up before a tenant opens
// and down only after it has closed, so Stats never under-reports.
func (m *Manager) handles(delta int) {
	m.mu.Lock()
	m.stats.Open += delta
	m.stats.PeakOpen = max(m.stats.PeakOpen, m.stats.Open)
	m.mu.Unlock()
}

func (m *Manager) release(t *Tenant) {
	m.mu.Lock()
	if t.refs--; t.refs == 0 {
		if !t.doomed {
			t.idle = m.idle.PushFront(t)
		}
		m.broadcast()
	}
	m.mu.Unlock()
}

// wait releases mu until the next broadcast or the end of ctx. mu is held
// again when it returns.
func (m *Manager) wait(ctx context.Context) error {
	ch := m.changed
	m.waiters++
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.waiters--
	}()
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) broadcast() {
	if m.waiters > 0 {
		close(m.changed)
		m.changed = make(chan struct{})
	}
}
