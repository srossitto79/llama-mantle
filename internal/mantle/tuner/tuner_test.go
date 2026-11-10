package tuner

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/mostlygeek/llama-swap/internal/configedit"
)

type stubProbe struct{}

func (stubProbe) Probe(ctx context.Context) (SystemInfo, error) {
	return SystemInfo{Hash: "hash", Threads: 8}, nil
}

type stubInspector struct {
	paths []string
	err   error
}

func (s *stubInspector) Inspect(path string) (ModelInfo, error) {
	s.paths = append(s.paths, path)
	if s.err != nil {
		return ModelInfo{}, s.err
	}
	return ModelInfo{Path: path}, nil
}

type stubResolver struct {
	model ModelInfo
	draft *ModelInfo
}

func (s *stubResolver) Resolve(model ModelInfo, draft *ModelInfo) ([]BackendFit, error) {
	s.model, s.draft = model, draft
	return []BackendFit{{Name: "b", Binary: "/b/llama-server"}}, nil
}

type stubPlanner struct {
	req Request
}

func (s *stubPlanner) Plans(sys SystemInfo, model ModelInfo, fits []BackendFit, req Request) []TierPlan {
	s.req = req
	var out []TierPlan
	for _, tier := range req.Tiers {
		out = append(out, TierPlan{Tier: tier, Candidates: []Candidate{
			{ID: "ok", Tier: tier, Backend: "b", Binary: "/b/llama-server"},
			{ID: "bad", Tier: tier, Backend: "b", Binary: "/b/llama-server", Args: []string{"bad"}},
			{ID: "blocked", Tier: tier, Backend: "b", Blocked: "no draft model support"},
		}})
	}
	return out
}

// stubProjector reports a launch that always fits.
type stubProjector struct{}

func (stubProjector) Project(ctx context.Context, bin string, args []string) (Projection, error) {
	return Projection{FreeMiB: 23300, TotalMiB: 1000}, nil
}

type stubProposer struct {
	calls int
}

func (s *stubProposer) Propose(model ModelInfo, c Candidate, req Request) (configedit.Change, error) {
	s.calls++
	for _, a := range c.Args {
		if a == "bad" {
			return configedit.Change{}, errors.New("cannot propose")
		}
	}
	return configedit.Change{Op: "add", Section: "models", ID: "entry-" + strconv.Itoa(c.Tier)}, nil
}

func newStubTuner() (Tuner, *stubInspector, *stubResolver, *stubPlanner, *stubProposer) {
	in, rs, pl, pr := &stubInspector{}, &stubResolver{}, &stubPlanner{}, &stubProposer{}
	tn := NewTuner(Deps{Probe: stubProbe{}, Inspector: in, Resolver: rs, Planner: pl, Projector: stubProjector{}, Proposer: pr})
	return tn, in, rs, pl, pr
}

func candidateByID(t *testing.T, tp TierPlan, id string) Candidate {
	t.Helper()
	for _, c := range tp.Candidates {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("candidate %q not found", id)
	return Candidate{}
}

func TestTuner_AnalyseAttachesEntries(t *testing.T) {
	tn, _, _, _, pr := newStubTuner()
	a, err := tn.Analyse(context.Background(), Request{Model: "/m/x.gguf", Tiers: []int{65536, 131072}})
	if err != nil {
		t.Fatal(err)
	}
	if a.System.Hash != "hash" || a.Model.Path != "/m/x.gguf" || len(a.Backends) != 1 {
		t.Fatalf("analysis header: %+v", a)
	}
	if len(a.Tiers) != 2 {
		t.Fatalf("tiers = %d", len(a.Tiers))
	}
	for _, tp := range a.Tiers {
		ok := candidateByID(t, tp, "ok")
		if ok.Entry == nil || ok.Entry.ID != "entry-"+strconv.Itoa(tp.Tier) || ok.Blocked != "" {
			t.Errorf("tier %d: ok candidate: %+v", tp.Tier, ok)
		}
		blocked := candidateByID(t, tp, "blocked")
		if blocked.Entry != nil || blocked.Blocked != "no draft model support" {
			t.Errorf("tier %d: blocked candidate: %+v", tp.Tier, blocked)
		}
	}
	// The blocked candidates are never offered to the proposer.
	if pr.calls != 4 {
		t.Errorf("proposer calls = %d, want 4", pr.calls)
	}
}

func TestTuner_AnalyseProposerErrorBlocksCandidate(t *testing.T) {
	tn, _, _, _, _ := newStubTuner()
	a, err := tn.Analyse(context.Background(), Request{Model: "/m/x.gguf", Tiers: []int{65536}})
	if err != nil {
		t.Fatal(err)
	}
	bad := candidateByID(t, a.Tiers[0], "bad")
	if bad.Blocked != "cannot propose" || bad.Entry != nil {
		t.Fatalf("bad candidate: %+v", bad)
	}
}

func TestTuner_AnalyseDefaultTiers(t *testing.T) {
	tn, _, _, pl, _ := newStubTuner()
	a, err := tn.Analyse(context.Background(), Request{Model: "/m/x.gguf"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(pl.req.Tiers, DefaultTiers) {
		t.Fatalf("planner tiers = %v, want %v", pl.req.Tiers, DefaultTiers)
	}
	if len(a.Tiers) != len(DefaultTiers) {
		t.Fatalf("tiers = %d", len(a.Tiers))
	}
}

func TestTuner_AnalyseKeepsRequestedTiers(t *testing.T) {
	tn, _, _, pl, _ := newStubTuner()
	if _, err := tn.Analyse(context.Background(), Request{Model: "/m/x.gguf", Tiers: []int{32768}}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(pl.req.Tiers, []int{32768}) {
		t.Fatalf("planner tiers = %v", pl.req.Tiers)
	}
}

func TestTuner_AnalyseInspectsDraft(t *testing.T) {
	tn, in, rs, _, _ := newStubTuner()
	if _, err := tn.Analyse(context.Background(), Request{Model: "/m/x.gguf", Draft: "/m/d.gguf"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in.paths, []string{"/m/x.gguf", "/m/d.gguf"}) {
		t.Fatalf("inspected %v", in.paths)
	}
	if rs.draft == nil || rs.draft.Path != "/m/d.gguf" {
		t.Fatalf("resolver draft = %+v", rs.draft)
	}

	if _, err := tn.Analyse(context.Background(), Request{Model: "/m/x.gguf"}); err != nil {
		t.Fatal(err)
	}
	if rs.draft != nil {
		t.Fatalf("resolver draft = %+v without a draft file", rs.draft)
	}
}

func TestTuner_AnalyseInspectorError(t *testing.T) {
	tn, in, _, _, _ := newStubTuner()
	in.err = errors.New("unreadable")
	if _, err := tn.Analyse(context.Background(), Request{Model: "/m/x.gguf"}); err == nil || err.Error() != "unreadable" {
		t.Fatalf("err = %v", err)
	}
}
