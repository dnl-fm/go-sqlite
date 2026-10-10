package writers

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Load contract: violations, verdicts, plateau and drain checks, result files.

func (m *loadMetrics) fail(err error) {
	if m.firstError == "" {
		m.firstError = err.Error()
	}
}
func absLoad(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Assertions and artifact verdicts use the same contract for the recorded mode.
func loadAssert(t *testing.T, r loadRow) {
	t.Helper()
	for _, failure := range loadViolations(r) {
		t.Error(failure)
	}
}

func loadViolations(r loadRow) []string {
	var failures []string
	fail := func(format string, args ...any) { failures = append(failures, fmt.Sprintf(format, args...)) }
	if r.Accepted == 0 || r.Reads == 0 || r.Transactions < 2 {
		fail("insufficient progress: accepted=%d reads=%d transactions=%d", r.Accepted, r.Reads, r.Transactions)
	}
	if r.Scheduled != r.Accepted+r.Rejected+r.Errors {
		fail("accounting mismatch: %d scheduled, %d outcomes", r.Scheduled, r.Accepted+r.Rejected+r.Errors)
	}
	if r.Lost != 0 || r.Errors != 0 || r.ReadErrors != 0 {
		fail("correctness: lost=%d errors=%d read errors=%d first=%q", r.Lost, r.Errors, r.ReadErrors, r.FirstError)
	}
	if r.Phase == "calibration" {
		if r.Rejected != 0 {
			fail("calibration deadline errors: %d", r.Rejected)
		}
		return failures
	}
	scale := time.Duration(1)
	if raceEnabled {
		scale = 10
	}
	// Every active cell must keep making commits, even compact bursts and C-FULL.
	if r.Gap > scale*250*time.Millisecond {
		fail("commit gap=%s ms (want <=%s)", ms(r.Gap), ms(scale*250*time.Millisecond))
	}
	switch r.Phase {
	case "50%", "80%":
		if r.Pattern == "C-FULL" {
			break
		} // sustained tails/deadlines recorded only
		if r.Rejected != 0 {
			fail("sustained deadline errors: %d", r.Rejected)
		}
		if r.Full && r.Phase == "50%" && (r.WP99 > r.Sync.bound() || r.WMax > 250*time.Millisecond || r.RP99 > 10*time.Millisecond) {
			fail("sustained bounds: write p99=%s max=%s reader p99=%s ms (want <=%s/250/10)", ms(r.WP99), ms(r.WMax), ms(r.RP99), ms(r.Sync.bound()))
		}
	case "burst":
		if r.Rejected != 0 {
			fail("burst deadline errors: %d", r.Rejected)
		}
		if r.Full {
			if r.WMax > 5*time.Second {
				fail("burst write max=%s ms (want <=5000)", ms(r.WMax))
			}
			if r.Drain < 0 {
				fail("burst drain to sustained bound %s ms not observed before cell ended", ms(r.Sync.bound()))
			} else if r.Drain > 8*time.Second {
				fail("burst drain=%s ms (want <=8000)", ms(r.Drain))
			}
		}
	case "burst-short-deadline", "overload":
		// Errors of any other type were counted as unexpected failures above.
		// The overload admission child is stricter than its outer request budget.
		rejectLimit := min(r.Deadline, r.Admission+50*time.Millisecond)
		if r.Phase == "burst-short-deadline" {
			rejectLimit = min(r.Deadline, r.Admission) + 50*time.Millisecond
		}
		if r.RejectMax > rejectLimit {
			fail("typed rejection max=%s ms (want <=%s)", ms(r.RejectMax), ms(rejectLimit))
		}
		if r.Phase == "overload" && r.Full {
			if r.WP99 > scale*250*time.Millisecond {
				fail("overload accepted p99=%s ms (want <=%s)", ms(r.WP99), ms(scale*250*time.Millisecond))
			}
			if !r.Plateau {
				fail("overload heap/goroutines did not plateau: %+v", r.Samples)
			}
		}
	}
	return failures
}

func loadVerdict(r loadRow) string {
	mode := "short correctness; latency/drain/plateau recorded"
	if r.Full {
		mode = "full contract"
	}
	if r.Pattern == "C-FULL" && (r.Phase == "50%" || r.Phase == "80%") {
		mode += "; C-FULL sustained tails/deadlines recorded"
	} else if r.Full && r.Phase == "80%" {
		mode += "; 80% sustained latency recorded"
	}
	verdict := "PASS"
	if failures := loadViolations(r); len(failures) > 0 {
		verdict = "RED: " + strings.Join(failures, "; ")
	}
	return fmt.Sprintf("%s (%s; sync p50/p99/max=%s/%s/%s ms; sustained bound=%s ms)", verdict, mode, ms(r.Sync.P50), ms(r.Sync.P99), ms(r.Sync.Max), ms(r.Sync.bound()))
}

func loadPlateau(samples []loadMemory, duration time.Duration) bool {
	// Ignore startup and the first deadline's accumulation. Compare maxima in
	// equal halves of the remaining run; heap is sawtoothed by ordinary GC.
	warm := min(time.Second, duration/4)
	var earlyHeap, lateHeap uint64
	earlyGo, lateGo := 0, 0
	earlyN, lateN := 0, 0
	heapFloor := [3]uint64{^uint64(0), ^uint64(0), ^uint64(0)}
	goFloor := [3]int{int(^uint(0) >> 1), int(^uint(0) >> 1), int(^uint(0) >> 1)}
	counts := [3]int{}
	middle := warm + (duration-warm)/2
	for _, s := range samples {
		if s.At < warm {
			continue
		}
		window := min(2, int(3*(s.At-warm)/(duration-warm)))
		counts[window]++
		heapFloor[window] = min(heapFloor[window], s.Heap)
		goFloor[window] = min(goFloor[window], s.Goroutines)
		if s.At < middle {
			earlyN++
			earlyHeap = max(earlyHeap, s.Heap)
			earlyGo = max(earlyGo, s.Goroutines)
		} else {
			lateN++
			lateHeap = max(lateHeap, s.Heap)
			lateGo = max(lateGo, s.Goroutines)
		}
	}
	// Peak bounds alone can bless a slow leak. Compare low-water marks in
	// three windows as well, allowing small allocator/scheduler noise. A
	// middle trough is bounded oscillation, not sustained monotonic growth.
	heapGrowth := heapFloor[1] > heapFloor[0] && heapFloor[2] > heapFloor[1] &&
		heapFloor[2] > heapFloor[0]+max(uint64(2<<20), heapFloor[0]/10)
	goGrowth := goFloor[1] > goFloor[0] && goFloor[2] > goFloor[1] &&
		goFloor[2] > goFloor[0]+max(16, goFloor[0]/10)
	return earlyN > 0 && lateN > 0 && counts[0] > 0 && counts[1] > 0 && counts[2] > 0 &&
		lateHeap <= earlyHeap+earlyHeap/2+(16<<20) && lateGo <= earlyGo+max(64, earlyGo/4) &&
		!heapGrowth && !goGrowth
}
func loadDrain(windows []loadWindow, duration, spike, windowDuration, bound time.Duration) time.Duration {
	spikeEnd := spike/2 + spike
	for i := int((spikeEnd + windowDuration - 1) / windowDuration); i+1 < len(windows); i++ {
		observed := time.Duration(i+2) * windowDuration
		complete := observed <= duration
		for _, w := range windows[i : i+2] {
			observed = max(observed, w.observed)
			complete = complete && w.scheduled > 0 && w.completed == w.scheduled && w.writes.n > 0 && w.writes.p99() <= bound
		}
		// Recovery is observed after both arrival windows close and their replies
		// return. A fast window completed after the cell ends cannot prove drain.
		if complete && observed <= duration {
			return observed - spikeEnd
		}
	}
	return -1
}

func loadTable(rows []loadRow) string {
	var b strings.Builder
	b.WriteString("| pattern | phase | capacity req/s | capacity tx/s | offered req/s | accepted | rejected | errors | lost | write p99 ms | write max ms | reject max ms | read p99 ms | gap ms | drain | plateau | verdict |\n|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | %s | %.0f | %.0f | %.0f | %d | %d | %d | %d | %s | %s | %s | %s | %s | %s | %t | %s |\n", r.Pattern, r.Phase, r.Capacity, r.CapacityTx, r.Rate, r.Accepted, r.Rejected, r.Errors+r.ReadErrors, r.Lost, ms(r.WP99), ms(r.WMax), ms(r.RejectMax), ms(r.RP99), ms(r.Gap), r.Drain, r.Plateau, loadVerdict(r))
	}
	return b.String()
}
func writeLoadResults(started time.Time) error {
	if err := os.MkdirAll("results", 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("pattern\tphase\tcapacity_req_s\tcapacity_tx_s\toffered_req_s\tseconds\telapsed_seconds\trequest_deadline_ms\tadmission_ms\tscheduled\taccepted\trejected\terrors\tread_errors\treads\ttransactions\tlost\twrite_p99_ms\twrite_max_ms\treject_max_ms\tread_p99_ms\tcommit_gap_ms\tdrain_ms\tplateau\theap_bytes_series\tgoroutines_series\tpending_series\tfirst_error\tfull_mode\tsync_p50_ms\tsync_p99_ms\tsync_max_ms\tsustained_bound_ms\tverdict\n")
	for _, r := range loadResults {
		var heap, gs, pending []string
		for _, s := range r.Samples {
			heap = append(heap, fmt.Sprint(s.Heap))
			gs = append(gs, fmt.Sprint(s.Goroutines))
			pending = append(pending, fmt.Sprint(s.Pending))
		}
		fmt.Fprintf(&b, "%s\t%s\t%.2f\t%.2f\t%.2f\t%.2f\t%.2f\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%s\t%s\t%s\t%s\t%s\t%s\t%t\t%s\t%s\t%s\t%s\t%t\t%s\t%s\t%s\t%s\t%s\n", r.Pattern, r.Phase, r.Capacity, r.CapacityTx, r.Rate, r.Duration.Seconds(), r.Elapsed.Seconds(), ms(r.Deadline), ms(r.Admission), r.Scheduled, r.Accepted, r.Rejected, r.Errors, r.ReadErrors, r.Reads, r.Transactions, r.Lost, ms(r.WP99), ms(r.WMax), ms(r.RejectMax), ms(r.RP99), ms(r.Gap), ms(r.Drain), r.Plateau, strings.Join(heap, ","), strings.Join(gs, ","), strings.Join(pending, ","), strings.ReplaceAll(r.FirstError, "\t", " "), r.Full, ms(r.Sync.P50), ms(r.Sync.P99), ms(r.Sync.Max), ms(r.Sync.bound()), loadVerdict(r))
	}
	if err := os.WriteFile(filepath.Join("results", started.Format("2006-01-02")+"-load.tsv"), []byte(b.String()), 0o644); err != nil {
		return err
	}
	md := fmt.Sprintf("Run of %s, %s, GOMAXPROCS %d, %s. Real ext4 `.labdata/`, 90%% 512-byte inserts, 10%% read-modify-write over 64 hot rows; eight `Store.Read` point readers pausing 1ms. Calibration uses 64 closed-loop submitters for 10s; capacity counts acknowledgements inside that interval, excluding drain. Open-loop write latency includes generator lag. Histograms are bounded and round percentiles up to 100us. If p99 reaches the 1s overflow bin, it reports the maximum as a conservative bound.\n\nCapacity is acknowledged requests/s; transaction throughput counts distinct committed transactions (D groups requests). Commit gaps approximate commit times from the first successful acknowledgement of each transaction, including active-window start/end gaps; they are not gaps between individual grouped replies. At scenario start, 200 fresh-file 4KiB appends followed by `fdatasync` on the same ext4 filesystem measure sync p50/p99/max; full 50%% sustained write p99 must be <=3 times that measured p99, write max <=250ms and reader p99 <=10ms for C-NORMAL, D-NORMAL and D-FULL. At 80%%, latency is recorded while zero rejections, correctness and commit gaps <=250ms remain asserted. C-FULL sustained tails/deadlines are recorded only. Both bursts use 50%% background and a 200%% total spike. In full mode the spike starts at 1s, lasts 2s, and leaves 10s of recovery in a 13s cell. The added work above background is 1.5 times capacity for 2s, or 3s of capacity's work; 50%% spare needs about 6s to drain it. The burst carries outer and inner 5s deadlines: zero rejections, write max <=5s, and observed recovery within 8s after spike end, before the cell ends. The identical burst-short-deadline uses 1s and permits only typed `context.DeadlineExceeded` rejections returning within 1050ms. Other requests carry an outer 1s deadline; overload adds a child admission deadline at 100ms, with typed rejected completions within 150ms. Short mode asserts correctness, zero unexpected errors, commit gaps (race-only 10x allowance), and fast typed rejections; latency/drain/plateau are recorded. Full mode additionally asserts 50%% sustained latency, burst latency/recovery, and overload plateau. Compact calibration/sustained/burst/spike/overload durations are 0.5/1.5/1/0.2/1.5s; the spike starts at 0.1s, leaving 0.7s recovery. Memory samples every 200ms; plateau ignores the first second, compares equal early/late windows: late maximum heap <= early maximum *1.5 +16MiB, late maximum goroutines <= early maximum +max(64,25%%). Three equal post-warm windows compare low-water marks. Monotonic growth across all three windows fails when total growth exceeds max(2MiB,10%%) for heap or max(16,10%%) for goroutines. Bounded oscillation remains subject to the peak caps. These finite-run tolerances do not prove absence of smaller leaks. Drain is measured from spike end until two complete post-spike arrival windows have write p99 <= the disk-relative sustained bound. Its observation time is the later of both windows closing and their last completion; both must occur before measurement ends. Windows are 250ms in full mode, 100ms in compact mode; -1ns means no observed drain. Every cell verdict includes the measured disk stats and derived bound and uses that cell's asserted mode.\n\n%s", started.Format(time.RFC3339), runtime.Version(), runtime.GOMAXPROCS(0), engine(), loadTable(loadResults))
	for _, r := range loadResults {
		if r.FirstError != "" {
			md += fmt.Sprintf("\n%s/%s: `%s`\n", r.Pattern, r.Phase, r.FirstError)
		}
	}
	latest := filepath.Join("results", "latest.md")
	old, _ := os.ReadFile(latest)
	const heading = "\n## load\n"
	head, rest, _ := strings.Cut(string(old), heading)
	tail := ""
	if i := strings.Index(rest, "\n## "); i >= 0 {
		tail = rest[i:]
	}
	return os.WriteFile(latest, []byte(strings.TrimRight(head, "\n")+heading+"\n"+strings.TrimRight(md, "\n")+"\n"+tail), 0o644)
}
