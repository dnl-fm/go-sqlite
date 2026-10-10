package writers

import (
	"strings"
	"testing"
	"time"
)

func TestLoadResultsRequireEveryCell(t *testing.T) {
	var complete, filtered []loadRow
	for _, pattern := range []string{"C-NORMAL", "D-NORMAL", "C-FULL", "D-FULL"} {
		for _, phase := range []string{"calibration", "50%", "80%", "burst", "burst-short-deadline", "overload"} {
			complete = append(complete, loadRow{Pattern: pattern, Phase: phase})
			filtered = append(filtered, loadRow{Pattern: "C-NORMAL", Phase: phase})
		}
	}
	if completeLoadResults(nil) || completeLoadResults(complete[:loadCells-1]) || completeLoadResults(filtered) {
		t.Fatal("empty, partial, or repeated filtered results can replace a full record")
	}
	if !completeLoadResults(complete) {
		t.Fatal("all cells must permit writing the record")
	}
	var oldCoverage []loadRow
	for _, r := range complete {
		if r.Phase != "burst-short-deadline" {
			oldCoverage = append(oldCoverage, r)
		}
	}
	if completeLoadResults(oldCoverage) {
		t.Fatal("a run missing short-deadline bursts must not replace the record")
	}
}

func TestLoadContractsByMode(t *testing.T) {
	base := loadRow{Pattern: "D-NORMAL", Phase: "50%", Full: true,
		Sync:      loadSync{P50: 10 * time.Millisecond, P99: 20 * time.Millisecond, Max: 30 * time.Millisecond},
		Scheduled: 10, Accepted: 10, Reads: 10, Transactions: 2,
		Deadline: time.Second, Admission: time.Second,
		WP99: 40 * time.Millisecond, WMax: 100 * time.Millisecond, RP99: time.Millisecond,
		Gap: time.Millisecond, Plateau: true, Drain: time.Second}
	for _, tc := range []struct {
		name   string
		change func(*loadRow)
		red    bool
	}{
		{"disk-relative sustained passes above old 25ms", func(r *loadRow) {}, false},
		{"disk-relative sustained fails above derived bound", func(r *loadRow) { r.WP99 = r.Sync.bound() + 1 }, true},
		{"full sustained write max unchanged", func(r *loadRow) { r.WMax = 250*time.Millisecond + 1 }, true},
		{"full sustained reader bound unchanged", func(r *loadRow) { r.RP99 = 10*time.Millisecond + 1 }, true},
		{"full 80% records all latency bounds", func(r *loadRow) {
			r.Phase = "80%"
			r.WP99, r.WMax, r.RP99 = time.Second, time.Second, time.Second
		}, false},
		{"full 80% retains no rejection", func(r *loadRow) { r.Phase = "80%"; r.Accepted--; r.Rejected++ }, true},
		{"full 80% retains exact counts", func(r *loadRow) { r.Phase = "80%"; r.Lost++ }, true},
		{"full 80% retains commit progress", func(r *loadRow) { r.Phase = "80%"; r.Gap = 3 * time.Second }, true},
		{"short records sustained latency", func(r *loadRow) { r.Full = false; r.WP99, r.WMax, r.RP99 = time.Second, time.Second, time.Second }, false},
		{"short sustained retains no rejection", func(r *loadRow) { r.Full = false; r.Accepted--; r.Rejected++ }, true},
		{"C-FULL sustained recorded", func(r *loadRow) {
			r.Pattern = "C-FULL"
			r.WP99, r.WMax, r.RP99 = time.Second, time.Second, time.Second
			r.Accepted--
			r.Rejected++
		}, false},
		{"full burst five second write max", func(r *loadRow) { r.Phase = "burst"; r.WMax = 5 * time.Second }, false},
		{"full burst write max fails", func(r *loadRow) { r.Phase = "burst"; r.WMax = 5*time.Second + 1 }, true},
		{"full burst requires recovery", func(r *loadRow) { r.Phase = "burst"; r.Drain = -1 }, true},
		{"full burst permits eight second drain", func(r *loadRow) { r.Phase = "burst"; r.Drain = 8 * time.Second }, false},
		{"full burst drain fails above eight seconds", func(r *loadRow) { r.Phase = "burst"; r.Drain = 8*time.Second + 1 }, true},
		{"full burst retains no rejection", func(r *loadRow) { r.Phase = "burst"; r.Accepted--; r.Rejected++ }, true},
		{"short records burst latency and drain", func(r *loadRow) { r.Full = false; r.Phase = "burst"; r.WMax = 6 * time.Second; r.Drain = -1 }, false},
		{"short records drain above eight seconds", func(r *loadRow) { r.Full = false; r.Phase = "burst"; r.Drain = 9 * time.Second }, false},
		{"short burst still disallows rejection", func(r *loadRow) { r.Full = false; r.Phase = "burst"; r.Accepted--; r.Rejected++ }, true},
		{"short burst checks commit gap", func(r *loadRow) { r.Full = false; r.Phase = "burst"; r.Gap = 3 * time.Second }, true},
		{"short-deadline burst permits fast rejections", func(r *loadRow) {
			r.Phase = "burst-short-deadline"
			r.Accepted--
			r.Rejected++
			r.RejectMax = 1050 * time.Millisecond
			r.Drain = -1
		}, false},
		{"short-deadline burst fails late rejection", func(r *loadRow) {
			r.Full = false
			r.Phase = "burst-short-deadline"
			r.Accepted--
			r.Rejected++
			r.RejectMax = 1050*time.Millisecond + 1
		}, true},
		{"overload respects child deadline", func(r *loadRow) {
			r.Phase = "overload"
			r.Admission = 100 * time.Millisecond
			r.Accepted--
			r.Rejected++
			r.RejectMax = 150 * time.Millisecond
		}, false},
		{"overload fails above child deadline plus 50ms", func(r *loadRow) {
			r.Full = false
			r.Phase = "overload"
			r.Admission = 100 * time.Millisecond
			r.Accepted--
			r.Rejected++
			r.RejectMax = 150*time.Millisecond + 1
		}, true},
		{"full overload requires plateau", func(r *loadRow) { r.Phase = "overload"; r.Plateau = false }, true},
		{"short overload records latency and plateau", func(r *loadRow) { r.Full = false; r.Phase = "overload"; r.WP99 = 4 * time.Second; r.Plateau = false }, false},
		{"short-deadline burst disallows untyped errors", func(r *loadRow) { r.Phase = "burst-short-deadline"; r.Accepted--; r.Errors++ }, true},
		{"short keeps exact counts", func(r *loadRow) { r.Full = false; r.Lost++ }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.change(&r)
			failures := loadViolations(r)
			if (len(failures) > 0) != tc.red {
				t.Fatalf("red=%t, want %t: %v", len(failures) > 0, tc.red, failures)
			}
			verdict := loadVerdict(r)
			if strings.HasPrefix(verdict, "RED:") != tc.red || !strings.Contains(verdict, "sync p50/p99/max=10.00/20.00/30.00 ms; sustained bound=60.00 ms") {
				t.Fatalf("verdict omits contract failure or disk stats: %s", verdict)
			}
			if !r.Full && !strings.Contains(verdict, "short correctness; latency/drain/plateau recorded") {
				t.Fatalf("short verdict claims full latency proof: %s", verdict)
			}
			if r.Full && r.Phase == "80%" && !strings.Contains(verdict, "80% sustained latency recorded") {
				t.Fatalf("80%% verdict claims sustained latency proof: %s", verdict)
			}
		})
	}
}

func TestLoadDrainRequiresCompleteObservedWindows(t *testing.T) {
	const duration, spike, window, bound = 13 * time.Second, 2 * time.Second, 250 * time.Millisecond, 60 * time.Millisecond
	makeWindow := func(observed time.Duration) loadWindow {
		w := loadWindow{scheduled: 2, completed: 2, observed: observed}
		w.writes.add(40 * time.Millisecond)
		w.writes.add(50 * time.Millisecond)
		return w
	}
	for _, tc := range []struct {
		name   string
		change func([]loadWindow)
		want   time.Duration
	}{
		{"observation includes both closed windows", func(w []loadWindow) {}, 500 * time.Millisecond},
		{"observation waits for last reply", func(w []loadWindow) { w[13].observed = 3540 * time.Millisecond }, 540 * time.Millisecond},
		{"replies after cell ends do not prove drain", func(w []loadWindow) { w[13].observed = duration + 1 }, -1},
		{"incomplete observations do not prove drain", func(w []loadWindow) { w[13].completed-- }, -1},
		{"two windows required", func(w []loadWindow) { w[13] = loadWindow{} }, -1},
		{"disk bound required in both windows", func(w []loadWindow) { w[13].writes.add(bound + loadTick) }, -1},
		{"partial last window does not prove drain", func(w []loadWindow) { w[51], w[52] = w[12], w[13]; w[12], w[13] = loadWindow{}, loadWindow{} }, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			windows := make([]loadWindow, 53)
			windows[12], windows[13] = makeWindow(3240*time.Millisecond), makeWindow(3490*time.Millisecond)
			tc.change(windows)
			if got := loadDrain(windows, duration, spike, window, bound); got != tc.want {
				t.Fatalf("drain=%s, want %s", got, tc.want)
			}
		})
	}
}

func TestLoadDrainRecoveryDurationDoesNotMoveSpike(t *testing.T) {
	// A 2s spike always ends at 3s. Extra recovery time must leave the same
	// first two post-spike arrival windows available for observation.
	for _, duration := range []time.Duration{13 * time.Second, 20 * time.Second} {
		windows := make([]loadWindow, int(duration/(250*time.Millisecond))+1)
		for i := 12; i <= 13; i++ {
			windows[i].scheduled, windows[i].completed = 1, 1
			windows[i].observed = time.Duration(i+1)*250*time.Millisecond + 10*time.Millisecond
			windows[i].writes.add(10 * time.Millisecond)
		}
		if got := loadDrain(windows, duration, 2*time.Second, 250*time.Millisecond, 20*time.Millisecond); got != 510*time.Millisecond {
			t.Fatalf("duration %s: drain=%s, want 510ms", duration, got)
		}
	}
}

func TestLoadPlateauRejectsSustainedGrowth(t *testing.T) {
	var steady, heapGrowth, goroutineGrowth []loadMemory
	for second := 1; second <= 20; second++ {
		steady = append(steady, loadMemory{At: time.Duration(second) * time.Second, Heap: uint64(32+second%3) << 20, Goroutines: 100 + second%5})
		heapGrowth = append(heapGrowth, loadMemory{At: time.Duration(second) * time.Second, Heap: uint64(32+second) << 20, Goroutines: 100})
		goroutineGrowth = append(goroutineGrowth, loadMemory{At: time.Duration(second) * time.Second, Heap: 32 << 20, Goroutines: 100 + second*2})
	}
	if !loadPlateau(steady, 20*time.Second) {
		t.Fatal("stationary samples with allocator noise must pass")
	}
	if loadPlateau(heapGrowth, 20*time.Second) || loadPlateau(goroutineGrowth, 20*time.Second) {
		t.Fatal("steady memory or goroutine growth is not a plateau")
	}
}

func TestLoadPlateauAllowsBoundedOscillation(t *testing.T) {
	// Recorded race samples: a middle trough followed by recovery is not
	// sustained growth. Heap falls while goroutines stay under the peak cap.
	heap := []uint64{2661264, 2092272, 3610888, 3292928, 3596840, 3412496, 3243232, 2611944, 2361536}
	gs := []int{29, 206, 204, 188, 165, 143, 179, 225, 211}
	var samples []loadMemory
	for i := range heap {
		samples = append(samples, loadMemory{At: time.Duration(i+1) * 200 * time.Millisecond, Heap: heap[i], Goroutines: gs[i]})
	}
	if !loadPlateau(samples, 2*time.Second) {
		t.Fatal("bounded oscillation is not sustained memory growth")
	}
}
