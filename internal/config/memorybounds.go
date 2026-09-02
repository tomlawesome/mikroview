// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// The bounds a live store.maxMemory change has to sit inside (#796).
// They live beside validateStore's own CFG-0011/CFG-0012 rules in
// validate.go deliberately: a figure arriving over PUT /api/settings/
// store and a figure read out of config.yaml are the same setting, and
// two places deciding what is acceptable is how they come to disagree.
//
// The difference is what happens at the edges. The config file's rules
// are forgiving on purpose -- a bad value there is clamped or warned
// about so a typo cannot cost an operator their monitoring at startup.
// A slider cannot be typo'd past its own ends, so the API refuses an
// out-of-range figure outright rather than quietly substituting one:
// silently applying something other than what the operator dragged to
// is worse than saying no.

// MinMaxMemory is the smallest budget the control offers. 32 MiB is
// roughly 53,000 events at AssumedBytesPerEvent -- small enough to be a
// real choice on a constrained host, large enough that the live view
// still has something to show. Below this the buffer stops being a
// window on recent traffic and becomes a curiosity.
const MinMaxMemory ByteSize = 32 << 20

// ResidentPerRingByte is the measured ring-to-resident overhead: a ring
// of N bytes costs roughly 1.47N once the Go runtime and process
// overhead are counted (#244). CFG-0012's warning and
// docs/configuration.md both quote this same figure, so what an operator
// reads in the reference, what the startup warning says, and what the
// slider will let them reach all agree.
const ResidentPerRingByte = 1.47

// fallbackHostMemory is the ceiling used when the host's memory cannot
// be read at all -- a non-Linux build, or a /proc and /sys this process
// is not allowed to see. 1 GiB is CFG-0012's own "worth a second look"
// threshold: a figure the project already treats as generous but not
// reckless, and the honest answer when we do not know what the machine
// has. Nothing is clamped down to it, so an operator whose config file
// already asks for more keeps what they asked for (see MaxMemoryCeiling).
const fallbackHostMemory ByteSize = 1 << 30

// MemoryBounds is the range PUT /api/settings/store will accept, and the
// range the settings slider draws. Computed once at startup -- host
// memory does not change under a running process often enough to be
// worth re-reading on every request, and a ceiling that moved while an
// operator was dragging the handle would be worse than a slightly stale
// one.
type MemoryBounds struct {
	// Min and Max are in bytes, as store.maxMemory is.
	Min ByteSize
	Max ByteSize
	// HostTotal is what the headroom rule was applied to: the cgroup
	// limit if this process is in one, otherwise the machine's RAM. Zero
	// when nothing could be read, in which case Max came from
	// fallbackHostMemory. Surfaced so the UI can say what the ceiling is
	// a share of rather than presenting a bare number.
	HostTotal ByteSize
	// Source names where HostTotal came from, for the startup log line.
	Source string
}

// MaxMemoryCeiling computes the largest store.maxMemory this host can be
// asked for.
//
// THE HEADROOM RULE, in one place so it can be quoted rather than
// re-derived:
//
//  1. Take the smallest positive of the cgroup v2 memory.max, the cgroup
//     v1 memory.limit_in_bytes, and /proc/meminfo's MemTotal. A
//     containerised mikroview is bounded by its cgroup, not by the
//     machine underneath it, and reading only MemTotal would offer an
//     operator a figure the kernel would OOM-kill them for taking.
//  2. Reserve headroom of the larger of 256 MiB and a quarter of that
//     total. Everything that is not the ring lives in the reserved part:
//     the Go runtime, the other persisted stores, the frontend bundle,
//     query working set, and whatever else shares the box. A flat
//     reserve alone is too thin on a large host (256 MiB of a 32 GiB box
//     is nothing); a percentage alone is too thin on a small one (a
//     quarter of 512 MiB leaves 128 MiB for everything). The larger of
//     the two is right at both ends.
//  3. Divide what is left by ResidentPerRingByte. store.maxMemory sizes
//     the ring, but the host pays the resident cost, so the ceiling has
//     to be expressed in the units the operator sets -- otherwise a
//     "maximum" of 2 GiB would really mean 2.9 GiB resident and the
//     slider's right-hand end would be a figure that cannot be run.
//  4. Floor the result at MinMaxMemory, so even a host with nothing to
//     spare still offers a usable (if single-valued) range rather than
//     an inverted one, and round down to a whole MiB so the figure the
//     operator sees is the figure that is stored.
//
// current is the figure already in effect (from the config file or the
// settings store). The ceiling is never computed below it: refusing to
// let an operator keep the value their instance is already running on
// would make the control unusable on exactly the deliberately-large
// deployments #244 decided to allow. Raising the ceiling to meet a
// running value is not an endorsement of it -- CFG-0012 has already said
// its piece at startup.
func MaxMemoryCeiling(current ByteSize) MemoryBounds {
	total, source := hostMemory()

	basis := total
	if basis <= 0 {
		basis = fallbackHostMemory
		source = "no host memory figure available"
	}

	max := ceilingFor(basis)
	if current > max {
		max = current
	}

	return MemoryBounds{Min: MinMaxMemory, Max: max, HostTotal: total, Source: source}
}

// ceilingFor is steps 2 to 4 of the headroom rule above, separated from
// the reading of /proc and /sys so the rule itself can be tested against
// a host size rather than only against whichever machine happens to be
// running the suite.
func ceilingFor(basis ByteSize) ByteSize {
	headroom := basis / 4
	if headroom < flatHeadroom {
		headroom = flatHeadroom
	}
	spare := basis - headroom
	max := ByteSize(float64(spare) / ResidentPerRingByte)
	max -= max % (1 << 20) // whole MiB, so the slider's end is a round figure

	if max < MinMaxMemory {
		max = MinMaxMemory
	}
	return max
}

// flatHeadroom is the "larger of" rule's flat half -- see
// MaxMemoryCeiling's step 2 for why a percentage alone is not enough on
// a small host.
const flatHeadroom ByteSize = 256 << 20

// ValidateMaxMemory reports why a proposed store.maxMemory is not
// acceptable, or nil if it is. The message is written to be shown to an
// operator, not only logged: it says what was asked for and what the
// range is, because "invalid" on its own leaves them guessing which end
// they hit.
func (b MemoryBounds) ValidateMaxMemory(v ByteSize) error {
	if v < b.Min {
		return fmt.Errorf("%s is below the smallest usable event buffer (%s)", v, b.Min)
	}
	if v > b.Max {
		return fmt.Errorf("%s is more than this host can spare for the event buffer (%s)", v, b.Max)
	}
	return nil
}

// hostMemory returns the memory ceiling this process is actually subject
// to, and a short phrase naming where it came from. Zero means nothing
// readable was found.
//
// cgroup limits win over the machine's RAM when both are readable, and
// the smaller of the two cgroup versions wins over the other, because
// the binding constraint is whichever is lowest -- a container with a
// 512 MiB limit on a 64 GiB host has 512 MiB, and offering the operator
// anything derived from the 64 GiB would be offering them an OOM kill.
func hostMemory() (ByteSize, string) {
	var best ByteSize
	var source string
	consider := func(v ByteSize, name string) {
		if v <= 0 {
			return
		}
		if best == 0 || v < best {
			best, source = v, name
		}
	}

	consider(readCgroupLimit("/sys/fs/cgroup/memory.max"), "cgroup v2 memory.max")
	consider(readCgroupLimit("/sys/fs/cgroup/memory/memory.limit_in_bytes"), "cgroup v1 memory.limit_in_bytes")
	consider(readMemTotal("/proc/meminfo"), "/proc/meminfo MemTotal")

	return best, source
}

// readCgroupLimit reads a cgroup memory limit file. "max" (v2) and the
// enormous sentinel v1 uses both mean "no limit", and are reported as
// zero so the caller falls through to the next source rather than
// offering the operator a ceiling of eight exabytes.
func readCgroupLimit(path string) ByteSize {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	text := strings.TrimSpace(string(raw))
	if text == "max" {
		return 0
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	// cgroup v1 writes an unlimited memory limit as a value near
	// PAGE_COUNTER_MAX rather than a word, so anything at or above a
	// petabyte is that sentinel and not a real limit.
	if n >= 1<<50 {
		return 0
	}
	return ByteSize(n)
}

// readMemTotal reads MemTotal (in KiB, per proc(5)) out of /proc/meminfo.
func readMemTotal(path string) ByteSize {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		rest, ok := strings.CutPrefix(line, "MemTotal:")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return 0
		}
		kib, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil || kib <= 0 {
			return 0
		}
		return ByteSize(kib) * 1024
	}
	return 0
}
