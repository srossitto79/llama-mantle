package loopguard

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Detection constants from Strata (server.py, LOOP_* constants).
const (
	ngramWidth    = 12    // words per passage
	recentWindow  = 2000  // recent words whose coverage is measured
	historyWords  = 30000 // how far back passages are counted
	tailChars     = 2048  // chars at the end checked for a short period
	tailMaxPeriod = 64    // longest period that reads as a fully repeated tail
	minRepeats    = 3     // a passage seen this often covers its words
)

// NewDetector returns a LoopDetector that reports a loop when coverage
// reaches threshold.
func NewDetector(threshold float64) LoopDetector {
	return &coverageDetector{threshold: threshold}
}

type coverageDetector struct {
	threshold float64
	text      strings.Builder
}

func (d *coverageDetector) Feed(text string) { d.text.WriteString(text) }

func (d *coverageDetector) Check() (bool, float64) {
	c := repeatCoverage(d.text.String())
	return c >= d.threshold, c
}

// repeatCoverage is Strata's reasoning_repeat_coverage: the share of the last
// 2,000 words inside 12-word passages seen at least three times among the last
// 30,000 words, or 1.0 when the last 2,048 chars are periodic with a period of
// at most 64 chars.
func repeatCoverage(text string) float64 {
	if periodicTail(text) {
		return 1.0
	}
	words := splitWords(strings.ToLower(text))
	if len(words) > historyWords {
		words = words[len(words)-historyWords:]
	}
	if len(words) < recentWindow {
		return 0
	}

	// Intern words so a passage is a comparable array of ids.
	ids := make([]int32, len(words))
	intern := make(map[string]int32, len(words)/4)
	for i, w := range words {
		id, ok := intern[w]
		if !ok {
			id = int32(len(intern))
			intern[w] = id
		}
		ids[i] = id
	}

	n := len(ids) - ngramWidth + 1
	counts := make(map[[ngramWidth]int32]int, n)
	key := func(i int) (k [ngramWidth]int32) {
		copy(k[:], ids[i:i+ngramWidth])
		return k
	}
	for i := 0; i < n; i++ {
		counts[key(i)]++
	}

	start := len(ids) - recentWindow
	covered := make([]bool, recentWindow)
	for i := start; i < n; i++ {
		if counts[key(i)] >= minRepeats {
			for j := i; j < i+ngramWidth; j++ {
				covered[j-start] = true
			}
		}
	}
	total := 0
	for _, c := range covered {
		if c {
			total++
		}
	}
	return float64(total) / float64(recentWindow)
}

// periodicTail reports whether the last tailChars characters (lowercased) are
// periodic with a period of 1..tailMaxPeriod.
func periodicTail(text string) bool {
	// Slice the last tailChars runes, then lower them, as Python does.
	cut := len(text)
	for n := 0; n < tailChars; n++ {
		if cut == 0 {
			return false
		}
		_, size := utf8.DecodeLastRuneInString(text[:cut])
		cut -= size
	}
	tail := []rune(strings.ToLower(text[cut:]))
	if len(tail) != tailChars {
		return false
	}
	for p := 1; p <= tailMaxPeriod; p++ {
		periodic := true
		for i := p; i < len(tail); i++ {
			if tail[i] != tail[i-p] {
				periodic = false
				break
			}
		}
		if periodic {
			return true
		}
	}
	return false
}

// splitWords matches Python's re.findall(r"\w+|[^\w\s]", text) with Unicode
// \w (letters, numbers, underscore) and \s.
func splitWords(text string) []string {
	var words []string
	start := -1
	for i, r := range text {
		if isWordRune(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			words = append(words, text[start:i])
			start = -1
		}
		if !isPySpace(r) {
			words = append(words, string(r))
		}
	}
	if start >= 0 {
		words = append(words, text[start:])
	}
	return words
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
}

// isPySpace matches Python's str.isspace, which also counts the ASCII
// separators 0x1c..0x1f.
func isPySpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}
