package tenants

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	schemaV1  = `CREATE TABLE events (id INTEGER PRIMARY KEY, writer INTEGER NOT NULL, seq INTEGER NOT NULL)`
	insertSQL = `INSERT INTO events (writer, seq) VALUES (?, ?)`
	countSQL  = `SELECT count(*) FROM events`
)

// A test that needs a second process re-executes the test binary with these
// set (the pattern from lab/turso-v072/helpers_test.go).
const (
	childEnv        = "TENANTS_LAB_CHILD" // role: "crash" or "open"
	childRootEnv    = "TENANTS_LAB_ROOT"
	childMaxOpenEnv = "TENANTS_LAB_MAXOPEN"
	recordPrefix    = "RECORD\t"
)

var (
	full    = os.Getenv("LAB_FULL") == "1"
	isChild = os.Getenv(childEnv) != ""
)

func childCommand(testName, role, root string, extraEnv ...string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^"+testName+"$")
	cmd.Env = append(os.Environ(), childEnv+"="+role, childRootEnv+"="+root)
	cmd.Env = append(cmd.Env, extraEnv...)
	return cmd
}

// runChild runs a test in a fresh process and adopts what it recorded.
func runChild(t *testing.T, testName, role, root string, extraEnv ...string) {
	t.Helper()
	out, err := childCommand(testName, role, root, extraEnv...).CombinedOutput()
	if err != nil {
		t.Fatalf("child %s failed: %v\n%s", testName, err, out)
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		if rest, ok := strings.CutPrefix(line, recordPrefix); ok {
			if f := strings.Split(rest, "\t"); len(f) == 4 {
				record(t, f[0], f[1], f[2], f[3])
			}
		}
	}
}

func TestMain(m *testing.M) {
	start := time.Now()
	code := m.Run()
	if full && !isChild && code == 0 {
		if err := writeResults(start, time.Now()); err != nil {
			fmt.Fprintln(os.Stderr, "write results:", err)
			code = 1
		}
	}
	os.Exit(code)
}

// labRoot returns a fresh directory on the lab's own filesystem. /tmp is
// tmpfs on the lab machine, which would make every disk number a RAM number.
func labRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(".", ".labdata-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func newManager(t *testing.T, root string, maxOpen int, steps ...string) *Manager {
	t.Helper()
	m, err := New(Config{Root: root, MaxOpen: maxOpen, Migrations: steps})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func mustCreate(t *testing.T, m *Manager, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if err := m.Create(context.Background(), id); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
}

func tenantIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("t%04d", i)
	}
	return ids
}

func insert(ctx context.Context, m *Manager, id string, writer, seq int) error {
	return m.With(ctx, id, func(t *Tenant) error {
		return t.Write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, insertSQL, writer, seq)
			return err
		})
	})
}

func queryInt(ctx context.Context, m *Manager, id, query string) (n int, err error) {
	err = m.With(ctx, id, func(t *Tenant) error {
		return t.Read(ctx, func(tx *sql.Tx) error { return tx.QueryRowContext(ctx, query).Scan(&n) })
	})
	return n, err
}

func mustQueryInt(t *testing.T, m *Manager, id, query string) int {
	t.Helper()
	n, err := queryInt(context.Background(), m, id, query)
	if err != nil {
		t.Fatalf("%s: %s: %v", id, query, err)
	}
	return n
}

func integrityCheck(t *testing.T, m *Manager, id string) {
	t.Helper()
	var res string
	err := m.With(context.Background(), id, func(tn *Tenant) error {
		return tn.Read(context.Background(), func(tx *sql.Tx) error {
			return tx.QueryRow("PRAGMA integrity_check").Scan(&res)
		})
	})
	if err != nil || res != "ok" {
		t.Fatalf("integrity_check %s = %q, %v", id, res, err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func percentile(d []time.Duration, p float64) time.Duration {
	if len(d) == 0 {
		return 0
	}
	s := slices.Clone(d)
	slices.Sort(s)
	return s[min(len(s)-1, int(float64(len(s))*p))]
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// rssAnon returns the process's anonymous resident memory in kB after
// returning everything Go can return to the OS.
func rssAnon(t *testing.T) int {
	t.Helper()
	runtime.GC()
	runtime.GC()
	debug.FreeOSMemory()
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(status), "\n") {
		if rest, ok := strings.CutPrefix(line, "RssAnon:"); ok {
			kb, err := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(rest, "kB")))
			if err != nil {
				t.Fatal(err)
			}
			return kb
		}
	}
	t.Fatal("no RssAnon in /proc/self/status")
	return 0
}

// openFiles returns the link targets of this process's file descriptors.
func openFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	var targets []string
	for _, e := range entries {
		if target, err := os.Readlink("/proc/self/fd/" + e.Name()); err == nil {
			targets = append(targets, target)
		}
	}
	return targets
}

// openTenantFiles counts distinct <id>.db files under root this process has
// open: the kernel's view of how many tenants are open. A sweep of
// /proc/self/fd is not atomic, so every counted descriptor is read a second
// time; if one changed meanwhile the sample is inconsistent and ok is false.
func openTenantFiles(t *testing.T, root string) (n int, ok bool) {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	fds := map[string]string{}
	for _, e := range entries {
		target, err := os.Readlink("/proc/self/fd/" + e.Name())
		if err == nil && strings.HasPrefix(target, root) && strings.HasSuffix(target, ".db") {
			fds[e.Name()] = target
		}
	}
	distinct := map[string]bool{}
	for fd, target := range fds {
		if now, err := os.Readlink("/proc/self/fd/" + fd); err != nil || now != target {
			return 0, false
		}
		distinct[target] = true
	}
	return len(distinct), true
}

// Results recorder: every scenario records its measurements; a full run
// writes them to results/.

type measurement struct{ scenario, metric, value, unit string }

var recorded struct {
	sync.Mutex
	rows []measurement
}

func record(t *testing.T, scenario, metric string, value any, unit string) {
	v := fmt.Sprint(value)
	if f, ok := value.(float64); ok {
		v = strconv.FormatFloat(f, 'f', 2, 64)
	}
	if isChild {
		fmt.Printf("%s%s\t%s\t%s\t%s\n", recordPrefix, scenario, metric, v, unit)
		return
	}
	t.Logf("%-18s %-56s %12s %s", scenario, metric, v, unit)
	recorded.Lock()
	recorded.rows = append(recorded.rows, measurement{scenario, metric, v, unit})
	recorded.Unlock()
}

func writeResults(start, end time.Time) error {
	meta := []measurement{
		{"run", "start", start.Format(time.RFC3339), ""},
		{"run", "end", end.Format(time.RFC3339), ""},
		{"run", "wall", strconv.FormatFloat(end.Sub(start).Seconds(), 'f', 1, 64), "s"},
		{"run", "go", runtime.Version(), ""},
		{"run", "modernc.org/sqlite", moduleVersion("modernc.org/sqlite"), ""},
		{"run", "cpus", strconv.Itoa(runtime.NumCPU()), ""},
		{"run", "race detector", strconv.FormatBool(raceEnabled), ""},
	}
	// Tests run in file order; the report reads better in scenario order.
	slices.SortStableFunc(recorded.rows, func(a, b measurement) int { return strings.Compare(a.scenario, b.scenario) })
	rows := append(meta, recorded.rows...)

	var tsv, md strings.Builder
	tsv.WriteString("scenario\tmetric\tvalue\tunit\n")
	md.WriteString("# sqlite-tenants: full run " + start.Format(time.DateOnly) + "\n")
	last := ""
	for _, r := range rows {
		fmt.Fprintf(&tsv, "%s\t%s\t%s\t%s\n", r.scenario, r.metric, r.value, r.unit)
		if r.scenario != last {
			fmt.Fprintf(&md, "\n## %s\n\n| metric | value | unit |\n|---|---:|---|\n", r.scenario)
			last = r.scenario
		}
		fmt.Fprintf(&md, "| %s | %s | %s |\n", r.metric, r.value, r.unit)
	}
	if err := os.MkdirAll("results", 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join("results", start.Format(time.DateOnly)+".tsv"), []byte(tsv.String()), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join("results", "latest.md"), []byte(md.String()), 0o644)
}

// moduleVersion reads the pinned version of a dependency from go.mod.
func moduleVersion(path string) string {
	gomod, _ := os.ReadFile("go.mod")
	for line := range strings.SplitSeq(string(gomod), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[len(f)-2] == path {
			return f[len(f)-1]
		}
	}
	return "unknown"
}
