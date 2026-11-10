package tuner

import (
	"fmt"
	"sort"
	"strconv"
)

// Planning constants. The planner only enumerates; memory is sized from
// llama.cpp's own projection (see size.go).
const (
	minCacheMiB   = 1024 // below this an expert cache is not worth the VRAM
	cacheStepMiB  = 256  // --moe-cache-mib is rounded down to this step
	maxCandidates = 6
	maxPerBackend = 4
	draftNMax     = 3
	// hostReserveMiB is the RAM left to the OS and the backend besides
	// pinned host experts.
	hostReserveMiB = 8192
)

// ubatchSizes are the prefill batch sizes tried. A larger ubatch speeds up
// prefill; its compute buffer, which on some architectures also grows with
// context, comes out of the expert cache.
var ubatchSizes = []int{512, 1024, 2048, 4096, 8192}

// NewPlanGenerator returns the analytical planner.
func NewPlanGenerator() PlanGenerator { return planner{} }

type planner struct{}

func mib(b int64) int64 { return b / (1 << 20) }

func (planner) Plans(sys SystemInfo, model ModelInfo, fits []BackendFit, req Request) []TierPlan {
	var budget int64
	if len(sys.GPUs) > 0 {
		// Only seeds the full-GPU check and -ncmoe; the projection decides.
		budget = sys.GPUs[0].TotalMiB
	}
	tiers := req.Tiers
	if len(tiers) == 0 {
		tiers = DefaultTiers
	}
	var out []TierPlan
	for _, tier := range tiers {
		tp := TierPlan{Tier: tier, Candidates: []Candidate{}}
		if model.TrainedContext > 0 && tier > model.TrainedContext && !req.RopeScale {
			tp.Skipped = fmt.Sprintf("Above trained context (%d)", model.TrainedContext)
			out = append(out, tp)
			continue
		}
		if host := hostExpertMiB(model, budget); sys.RAMMiB > 0 && host+hostReserveMiB > sys.RAMMiB {
			tp.Skipped = fmt.Sprintf("Experts exceed RAM: %d GiB, %d GiB available", roundUp(host, 1024)/1024, (sys.RAMMiB-hostReserveMiB)/1024)
			out = append(out, tp)
			continue
		}
		var all, blocked []Candidate
		usable := 0
		for bi, fit := range fits {
			if fit.Blocked != "" {
				continue
			}
			usable++
			cs, bl := backendCandidates(sys, model, fit, tier, budget, req)
			for i := range cs {
				cs[i].ID = fmt.Sprintf("%dk-%d-%d", tier/1024, bi, i)
			}
			for i := range bl {
				bl[i].ID = fmt.Sprintf("%dk-%d-b%d", tier/1024, bi, i)
			}
			all = append(all, cs...)
			blocked = append(blocked, bl...)
		}
		// Every option is kept: the sizer drops what does not fit, then pick
		// trims to the shown set.
		tp.Candidates = append(all, dedupeBlocked(blocked)...)
		switch {
		case usable == 0:
			tp.Skipped = "No usable backend"
		case len(tp.Candidates) == 0:
			tp.Skipped = "No layout fits in VRAM"
		}
		out = append(out, tp)
	}
	return out
}

// hostExpertMiB is the least expert memory any layout leaves in host RAM: the
// experts that do not fit in the whole GPU.
func hostExpertMiB(model ModelInfo, budget int64) int64 {
	if !model.IsMoE() {
		return 0
	}
	return max(mib(model.ExpertBytes)-budget, 0)
}

// backendCandidates returns the candidates of one backend for one tier and
// the ones blocked by a draft verdict. Memory-dependent values (cache size,
// -ncmoe) are seeds the sizer replaces from projections.
func backendCandidates(sys SystemInfo, model ModelInfo, fit BackendFit, tier int, budget int64, req Request) (ok, blocked []Candidate) {
	kv := mib(model.KVBytesPerTokenQ8 * int64(tier))
	dense := mib(model.DenseBytes)
	experts := mib(model.ExpertBytes)

	drafts := draftOptions(model, fit, req)
	for _, d := range drafts {
		if d.blocked != "" {
			blocked = append(blocked, Candidate{
				Tier: tier, Backend: fit.Name, Args: d.args, Reason: d.reason, Blocked: d.blocked,
			})
		}
	}

	for _, ub := range ubatchSizes {
		base := []string{"-ngl", "99", "-b", strconv.Itoa(max(ub, 8192)), "-ub", strconv.Itoa(ub)}
		var layout Layout
		var args []string
		var reason string
		switch {
		case dense+experts+kv <= budget:
			layout, reason = LayoutFullGPU, "Fits in VRAM"
		case model.IsMoE() && fit.Features[FeatureMoECache] && fit.Features[FeatureCPUMoE]:
			layout = LayoutMoECache
			args = []string{"-cmoe", "--moe-cache-mib", strconv.Itoa(minCacheMiB)}
			if sys.RAMMiB > 0 && experts+hostReserveMiB > sys.RAMMiB {
				// Every expert lives in host RAM. -lm none would fail the
				// pinned allocation at load; mmap pages experts from disk on
				// every token (Groot 280B: 0.12 tok/s prefill).
				continue
			}
			if fit.Features[FeatureLoadMode] {
				// Pinned host experts upload faster than pageable mmap.
				args = append(args, "-lm", "none")
			}
			reason = "Experts in host RAM"
		case model.IsMoE() && fit.Features[FeatureNCPUMoE]:
			perLayer := experts / int64(max(model.Layers, 1))
			if perLayer == 0 {
				continue
			}
			onGPU := min(max(int((budget-dense-kv)/perLayer), 0), model.Layers)
			layout = LayoutNCPUMoE
			args = []string{"-ncmoe", strconv.Itoa(model.Layers - onGPU)}
			reason = "Experts on CPU"
		case !model.IsMoE() && fit.Features[FeatureFit]:
			layout, reason = LayoutFit, "Layers split by --fit"
			base = []string{"-b", strconv.Itoa(max(ub, 8192)), "-ub", strconv.Itoa(ub)}
			args = []string{"--fit", "on"}
		default:
			continue
		}

		for _, d := range drafts {
			if d.blocked != "" {
				continue
			}
			a := append(append(append([]string{}, base...), args...), d.args...)
			a = append(a, "--threads", strconv.Itoa(max(sys.Threads, 1)))
			ok = append(ok, Candidate{
				Tier: tier, Backend: fit.Name, Binary: fit.Binary, Layout: layout, Args: a,
				Reason: joinReason(reason, d.reason), layoutReason: reason, draftReason: d.reason,
				Tunable: layout == LayoutMoECache && fit.Features[FeatureMoECacheTune],
			})
		}
	}
	return ok, blocked
}

func joinReason(layout, draft string) string {
	if draft == "" {
		return layout
	}
	return layout + "; " + draft
}

type draftOption struct {
	args    []string
	reason  string
	blocked string
}

// draftOptions lists the drafting modes to try, cheapest-to-win first.
// Single-user decoding is memory-bound, so a cheap draft usually pays.
func draftOptions(model ModelInfo, fit BackendFit, req Request) []draftOption {
	nmax := []string{}
	if fit.Features[FeatureSpecDraftNMax] {
		nmax = []string{"--spec-draft-n-max", strconv.Itoa(draftNMax)}
	}
	var opts []draftOption
	switch {
	case req.Draft != "" && fit.Draft != nil && !fit.Draft.OK:
		opts = append(opts, draftOption{args: []string{"--spec-draft-model", req.Draft}, reason: "Draft model", blocked: fit.Draft.Reason})
	case req.Draft != "" && fit.Draft != nil && fit.Draft.MTP:
		opts = append(opts, draftOption{
			args:   append([]string{"--spec-type", "draft-mtp", "--spec-draft-model", req.Draft}, nmax...),
			reason: "MTP file",
		})
	case req.Draft != "" && fit.Draft != nil:
		opts = append(opts, draftOption{
			args:   append([]string{"--spec-draft-model", req.Draft}, nmax...),
			reason: "Draft model",
		})
	case model.NextNLayers > 0 && fit.Features[FeatureSpecType]:
		opts = append(opts, draftOption{args: append([]string{"--spec-type", "draft-mtp"}, nmax...), reason: "MTP"})
	}
	usable := len(opts) > 0 && opts[0].blocked == ""
	if fit.Features[FeatureSpecType] && !usable {
		opts = append(opts, draftOption{args: []string{"--spec-type", "ngram-mod"}, reason: "n-gram drafts"})
	}
	opts = append(opts, draftOption{})
	return opts
}

var layoutRank = map[Layout]int{LayoutFullGPU: 0, LayoutMoECache: 1, LayoutNCPUMoE: 2, LayoutFit: 3}

// ubRank orders batch sizes whose cache lands in the same GiB: 2048 and
// 4096 are the usual prefill optimum, 512 is the last resort.
var ubRank = map[int]int{2048: 0, 4096: 1, 1024: 2, 8192: 3, 512: 4}

// pick keeps at most maxCandidates of one tier's sized candidates: best
// layout first, then the larger expert cache or fewer CPU layers, at most
// maxPerBackend per backend so another backend's best option still shows.
// Caches within the same GiB of the tier's largest count as equal, so the
// batch size decides between them.
func pick(all []Candidate) []Candidate {
	best := 0
	for _, c := range all {
		best = max(best, argInt(c, "--moe-cache-mib"))
	}
	gap := func(c Candidate) int { return (best - argInt(c, "--moe-cache-mib")) / 1024 }
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if layoutRank[a.Layout] != layoutRank[b.Layout] {
			return layoutRank[a.Layout] < layoutRank[b.Layout]
		}
		if ga, gb := gap(a), gap(b); ga != gb {
			return ga < gb
		}
		if na, nb := argInt(a, "-ncmoe"), argInt(b, "-ncmoe"); na != nb {
			return na < nb
		}
		if a.Tunable != b.Tunable {
			return a.Tunable
		}
		// Batch size before drafting, so the kept set pairs each batch size
		// with and without drafts instead of four draft variants.
		if ua, ub := ubRank[ubOf(a)], ubRank[ubOf(b)]; ua != ub {
			return ua < ub
		}
		return draftRank(a) < draftRank(b)
	})
	out := []Candidate{}
	per := map[string]int{}
	for _, c := range all {
		if len(out) == maxCandidates {
			break
		}
		if per[c.Backend] == maxPerBackend {
			continue
		}
		per[c.Backend]++
		out = append(out, c)
	}
	return out
}

func dedupeBlocked(bl []Candidate) []Candidate {
	out := []Candidate{}
	seen := map[string]bool{}
	for _, c := range bl {
		k := c.Backend + "|" + c.Blocked
		if !seen[k] {
			seen[k] = true
			out = append(out, c)
		}
	}
	return out
}

func draftRank(c Candidate) int {
	for i, a := range c.Args {
		if a == "--spec-type" && i+1 < len(c.Args) && c.Args[i+1] == "ngram-mod" {
			return 1
		}
		if a == "--spec-type" || a == "--spec-draft-model" {
			return 0
		}
	}
	return 2
}

func ubOf(c Candidate) int { return argInt(c, "-ub") }

// argInt returns the integer value of flag in c.Args, or 0.
func argInt(c Candidate, flag string) int {
	for i, a := range c.Args {
		if a == flag && i+1 < len(c.Args) {
			n, _ := strconv.Atoi(c.Args[i+1])
			return n
		}
	}
	return 0
}

// setArg replaces the value of flag in c.Args.
func setArg(c *Candidate, flag string, v int) {
	for i, a := range c.Args {
		if a == flag && i+1 < len(c.Args) {
			c.Args[i+1] = strconv.Itoa(v)
			return
		}
	}
}
