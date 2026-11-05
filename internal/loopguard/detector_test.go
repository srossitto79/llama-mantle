package loopguard

import (
	"compress/gzip"
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
)

type coverageFixture struct {
	Name     string  `json:"name"`
	Text     string  `json:"text"`
	Coverage float64 `json:"coverage"`
}

// Fixtures hold the output of Strata's reasoning_repeat_coverage on fixed
// inputs; testdata/gen_coverage_fixtures.py regenerates them.
func loadCoverageFixtures(t *testing.T) []coverageFixture {
	t.Helper()
	f, err := os.Open("testdata/coverage_fixtures.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var out []coverageFixture
	if err := json.NewDecoder(zr).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("no fixtures")
	}
	return out
}

func TestLoopDetector_MatchesStrata(t *testing.T) {
	for _, fx := range loadCoverageFixtures(t) {
		t.Run(fx.Name, func(t *testing.T) {
			got := repeatCoverage(fx.Text)
			if math.Abs(got-fx.Coverage) > 1e-9 {
				t.Errorf("coverage = %v, Strata = %v", got, fx.Coverage)
			}
		})
	}
}

func TestLoopDetector_Thresholds(t *testing.T) {
	var varied strings.Builder
	for i := 0; i < 3000; i++ {
		varied.WriteString("w")
		varied.WriteString(strings.Repeat("x", i%7))
		varied.WriteString(string(rune('a' + i%26)))
		varied.WriteString(string(rune('a' + (i/26)%26)))
		varied.WriteString(" ")
	}
	passage := "now let me check the sum again carefully, adding each term in order and comparing totals . "
	looped := varied.String()[:2000] + strings.Repeat(passage, 300)

	tests := []struct {
		name    string
		text    string
		looping bool
	}{
		{"varied text", varied.String(), false},
		{"repeated passage", looped, true},
		{"short repeated text", strings.Repeat(passage, 50), false},
		{"periodic digits", strings.Repeat("0123456789", 300), true},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewDetector(0.25)
			// Feed in uneven pieces, as streamed deltas arrive.
			for i := 0; i < len(tt.text); i += 7 {
				d.Feed(tt.text[i:min(i+7, len(tt.text))])
			}
			looping, cov := d.Check()
			if looping != tt.looping {
				t.Errorf("looping = %v (coverage %v), want %v", looping, cov, tt.looping)
			}
		})
	}
}

func TestLoopDetector_SplitWords(t *testing.T) {
	got := splitWords("perché x²=½; foo_bar città—ok")
	want := []string{"perché", "x²", "=", "½", ";", "foo_bar", "città", "—", "ok"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("splitWords = %q, want %q", got, want)
	}
}
