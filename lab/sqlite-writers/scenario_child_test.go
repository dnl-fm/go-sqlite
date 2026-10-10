package writers

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Strategy E: the multi-process runner and its re-executed child.

// runProcesses runs each plan in its own child process (the test binary
// re-executed), all against one database file, all using Open.
func runProcesses(t *testing.T, dir, dbPath, synchronous string, ps []plan) sample {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	type child struct {
		cmd *exec.Cmd
		out *bytes.Buffer
	}
	children := make([]child, len(ps))
	for i, p := range ps {
		var job bytes.Buffer
		err := gob.NewEncoder(&job).Encode(childJob{Plan: p, Dir: dir, DB: dbPath, Synchronous: synchronous})
		if err != nil {
			t.Fatal(err)
		}
		jobPath := filepath.Join(dir, fmt.Sprintf("job.%d", i))
		err = os.WriteFile(jobPath, job.Bytes(), 0o644)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestScenarioChild$")
		cmd.Env = append(os.Environ(), childEnv+"="+jobPath)
		out := &bytes.Buffer{}
		cmd.Stdout, cmd.Stderr = out, out
		err = cmd.Start()
		if err != nil {
			t.Fatal(err)
		}
		children[i] = child{cmd: cmd, out: out}
	}
	// Every child has the database open before any of them starts writing.
	for i := range ps {
		waitForFile(t, filepath.Join(dir, fmt.Sprintf("ready.%d", i)))
	}
	err := os.WriteFile(filepath.Join(dir, "go"), nil, 0o644)
	if err != nil {
		t.Fatal(err)
	}

	var total sample
	for i, c := range children {
		err := c.cmd.Wait()
		if err != nil {
			t.Fatalf("child %d: %v\n%s", i, err, c.out)
		}
		raw, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("out.%d", i)))
		if err != nil {
			t.Fatal(err)
		}
		var s sample
		err = gob.NewDecoder(bytes.NewReader(raw)).Decode(&s)
		if err != nil {
			t.Fatal(err)
		}
		total.merge(s)
	}
	return total
}

type childJob struct {
	Plan        plan
	Dir, DB     string
	Synchronous string
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

// TestScenarioChild is the body of a child process of strategy E. It does
// nothing in a normal test run.
func TestScenarioChild(t *testing.T) {
	jobPath := os.Getenv(childEnv)
	if jobPath == "" {
		t.Skip("child process of the multi-process scenario")
	}
	raw, err := os.ReadFile(jobPath)
	if err != nil {
		t.Fatal(err)
	}
	var job childJob
	err = gob.NewDecoder(bytes.NewReader(raw)).Decode(&job)
	if err != nil {
		t.Fatal(err)
	}
	tg, err := openSplit(job.DB, job.Synchronous, false)
	if err != nil {
		t.Fatal(err)
	}
	defer tg.close()

	err = os.WriteFile(filepath.Join(job.Dir, fmt.Sprintf("ready.%d", job.Plan.Process)), nil, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	waitForFile(t, filepath.Join(job.Dir, "go"))
	s := runPlan(job.Plan, tg, job.DB)

	var out bytes.Buffer
	err = gob.NewEncoder(&out).Encode(s)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(job.Dir, fmt.Sprintf("out.%d", job.Plan.Process)), out.Bytes(), 0o644)
	if err != nil {
		t.Fatal(err)
	}
}
