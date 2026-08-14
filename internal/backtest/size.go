package backtest

import (
	"fmt"
	"hash/fnv"
	"math/rand"
	"sort"

	"github.com/EzraStone/Lectio/internal/index"
	"github.com/EzraStone/Lectio/internal/rank"
)

// FileSizes measures every indexed file, in lines spanned by its non-test
// symbols.
//
// One definition, used by the largest-files baseline, the size-proportional
// control, and the quartile stratification. Gate A's first run at scale turned
// on the claim "the winning strategy is size", so every part of the report that
// argues about size has to be arguing about the same number.
func FileSizes(v *index.View) map[string]int {
	size := make(map[string]int)
	for _, sym := range v.Symbols {
		if sym.IsTest() {
			continue
		}
		size[sym.File] += sym.Lines()
	}
	return size
}

// Controls returns strategies that are scored and reported but do not decide
// the gate.
//
// The spec names four baselines and the gate is to beat all four. Adding a
// fifth to the pass condition would be moving the goalposts mid-run, so
// controls are kept structurally separate: they exist to explain a result, not
// to judge it.
func Controls() []Baseline {
	return []Baseline{SizeProportional{}}
}

// SizeProportional draws files at random with probability proportional to
// their size.
//
// The question it answers is narrower than the largest-files baseline's, and
// more useful after a run where largest-files won. Largest-files is the
// extreme of a size strategy: strictly the biggest, in order. This one is
// size-aware without being top-heavy, so the gap between the two says how much
// of largest-files' score comes from *being large* versus from *being the
// largest*. If a random size-weighted draw already scores near lectio, the
// ranking is contributing little beyond a size prior.
//
// The draw is seeded from the file set, so it is reproducible: the same index
// yields the same sample on every run, which is the property that lets a Gate
// A number be quoted. It is still a single draw per case and therefore noisier
// than the deterministic strategies — read it across the corpus, not per case.
type SizeProportional struct{}

// Name implements Baseline.
func (SizeProportional) Name() string { return "size-proportional draw" }

// RankFiles implements Baseline.
func (SizeProportional) RankFiles(v *index.View, _ rank.Params) []string {
	return weightedShuffle(FileSizes(v))
}

// weightedShuffle orders paths by successive weighted draws without
// replacement, so position one is proportional to weight, position two is
// proportional to weight among the rest, and so on.
func weightedShuffle(weights map[string]int) []string {
	paths := make([]string, 0, len(weights))
	var total int
	for p, w := range weights {
		if w > 0 {
			paths = append(paths, p)
			total += w
		}
	}
	// Sorting before drawing is what makes the seed meaningful: map iteration
	// order is randomized per process, so an unsorted candidate list would
	// produce a different sample every run from the same seed.
	sort.Strings(paths)

	h := fnv.New64a()
	for _, p := range paths {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
	}
	rng := rand.New(rand.NewSource(int64(h.Sum64())))

	out := make([]string, 0, len(paths))
	remaining := total
	for len(paths) > 0 {
		target := rng.Intn(remaining) + 1
		var cum, pick int
		for i, p := range paths {
			cum += weights[p]
			if cum >= target {
				pick = i
				break
			}
		}
		chosen := paths[pick]
		out = append(out, chosen)
		remaining -= weights[chosen]
		paths = append(paths[:pick], paths[pick+1:]...)
	}
	return out
}

// NumStrata is how many size bands the stratified report uses. Quartiles.
const NumStrata = 4

// StratumLabels name the bands smallest-first, for a report header.
var StratumLabels = [NumStrata]string{"Q1 smallest", "Q2", "Q3", "Q4 largest"}

// Strata assigns every sized file to a size quartile, 0 smallest to 3 largest.
//
// Split by rank rather than by value. Size distributions here are heavy-tailed
// — one file spans two thousand lines while the median spans forty — so
// equal-width value bands would put ninety percent of files in the bottom
// bucket and one file in the top, which is a histogram of the tail rather than
// a stratification.
func Strata(sizes map[string]int) map[string]int {
	paths := make([]string, 0, len(sizes))
	for p := range sizes {
		paths = append(paths, p)
	}
	sort.Slice(paths, func(i, j int) bool {
		if sizes[paths[i]] != sizes[paths[j]] {
			return sizes[paths[i]] < sizes[paths[j]]
		}
		return paths[i] < paths[j]
	})

	out := make(map[string]int, len(paths))
	n := len(paths)
	for i, p := range paths {
		q := i * NumStrata / max(n, 1)
		if q >= NumStrata {
			q = NumStrata - 1
		}
		out[p] = q
	}
	return out
}

// minStratumK is the smallest cutoff worth measuring precision at.
//
// Below three, precision within a stratum takes only four distinct values and
// one lucky hit swings it by a third.
const minStratumK = 3

// stratumK is the cutoff used inside one stratum: at most half its candidate
// files, and never more than the run's k.
//
// Halving is what keeps the comparison about ordering. A stratum holding
// twelve files scored at precision@10 gives every strategy nearly the same
// answer, because any ten of twelve files cover the same ground — the metric
// stops measuring which files were chosen and starts measuring how many exist.
// Returning zero means this stratum is too small to say anything on this case.
func stratumK(candidates, k int) int {
	kq := candidates / 2
	if kq > k {
		kq = k
	}
	if kq < minStratumK {
		return 0
	}
	return kq
}

// BandComparison is lectio against the largest-files baseline inside one size
// band.
type BandComparison struct {
	Stratum int
	Label   string
	Lectio  float64
	Largest float64
	Cases   int
}

// Won reports whether lectio outscored the baseline in this band.
func (b BandComparison) Won() bool { return b.Lectio > b.Largest }

// SizeReading interprets the stratified table, which exists to settle one
// question: when largest-files wins, is it choosing better files or just
// bigger ones?
type SizeReading struct {
	OverallLectio  float64
	OverallLargest float64
	Bands          []BandComparison
	// LectioWins counts bands where lectio scored higher.
	LectioWins int
	// Note states which reading the numbers support, in words.
	Note string
}

// ReadStrata compares lectio to the largest-files baseline band by band.
//
// The pattern worth naming is Simpson's paradox: largest-files ahead overall
// while behind inside every band. That happens when the ranking picks better
// files at each size and still loses, because the metric's ground truth is
// itself size-weighted — a newcomer touches big files because that is where
// the code is. Overall precision then rewards a strategy for the composition
// of its top ten rather than for its choices, and a size strategy wins by
// construction rather than by merit.
//
// The opposite pattern is equally decisive in the other direction. If
// largest-files also wins band by band, size is not an artifact of the metric
// and the ranking is genuinely behind.
func ReadStrata(rep Report) SizeReading {
	const incumbent = "largest files"

	var r SizeReading
	for _, a := range rep.Aggregates {
		switch a.Strategy {
		case DefaultVariant:
			r.OverallLectio = a.PrecisionA
		case incumbent:
			r.OverallLargest = a.PrecisionA
		}
	}

	type cell struct {
		p float64
		n int
	}
	lectio := map[int]cell{}
	largest := map[int]cell{}
	for _, s := range rep.Strata {
		switch s.Strategy {
		case DefaultVariant:
			lectio[s.Stratum] = cell{s.Precision, s.Cases}
		case incumbent:
			largest[s.Stratum] = cell{s.Precision, s.Cases}
		}
	}

	for q := 0; q < NumStrata; q++ {
		l, okL := lectio[q]
		b, okB := largest[q]
		if !okL || !okB {
			continue
		}
		cmp := BandComparison{
			Stratum: q, Label: StratumLabels[q],
			Lectio: l.p, Largest: b.p, Cases: min(l.n, b.n),
		}
		r.Bands = append(r.Bands, cmp)
		if cmp.Won() {
			r.LectioWins++
		}
	}

	r.Note = readingNote(r)
	return r
}

func readingNote(r SizeReading) string {
	n := len(r.Bands)
	switch {
	case n == 0:
		return "no size band had enough files and enough ground truth to score"
	case r.OverallLectio > r.OverallLargest:
		return fmt.Sprintf("lectio leads overall and in %d of %d size bands", r.LectioWins, n)
	case r.LectioWins == n:
		return fmt.Sprintf(
			"largest files leads overall while losing all %d size bands — the metric is "+
				"rewarding size, not better choices", n)
	case r.LectioWins == 0:
		return fmt.Sprintf(
			"largest files leads in all %d size bands — its advantage is not an artifact "+
				"of the metric", n)
	default:
		return fmt.Sprintf(
			"largest files leads overall; lectio wins %d of %d size bands — mixed, and not "+
				"enough to settle it", r.LectioWins, n)
	}
}

// StratumScore is one strategy's precision inside one size band on one case.
type StratumScore struct {
	Strategy string
	Stratum  int
	// K is the cutoff actually used, which varies with the stratum's size.
	K         int
	Precision float64
	// Support is how many touched files fell in this band. A stratum with no
	// support is skipped rather than scored zero — the strategy did not miss
	// anything there, there was nothing to hit.
	Support int
}

// scoreStrata splits one case's prediction and ground truth by size band and
// scores each band separately.
//
// This is the test that separates the two readings of the first run. If
// largest-files beats lectio overall but not inside any single band, then size
// is doing the work through the composition of the top ten rather than through
// better choices — the metric rewards size and a size strategy wins by
// construction. If largest-files also wins band by band, it is genuinely
// choosing better files and the ranking is the problem.
func scoreStrata(predicted, actual []string, sizes map[string]int, k int) []StratumScore {
	bands := Strata(sizes)

	// Candidate counts per band, over everything the strategy could have named.
	candidates := make([]int, NumStrata)
	for _, q := range bands {
		candidates[q]++
	}

	want := toSet(actual)
	support := make([]int, NumStrata)
	for f := range want {
		if q, ok := bands[f]; ok {
			support[q]++
		}
	}

	out := make([]StratumScore, 0, NumStrata)
	for q := 0; q < NumStrata; q++ {
		kq := stratumK(candidates[q], k)
		if kq == 0 || support[q] == 0 {
			continue
		}
		var named, hits int
		for _, f := range predicted {
			if bands[f] != q {
				continue
			}
			named++
			if want[f] {
				hits++
			}
			if named == kq {
				break
			}
		}
		// Divided by kq, not by how many the strategy managed to name. A
		// strategy that ranks only two files in a band and hits both has not
		// earned 100% — PrecisionAt's shrink-k fallback would hand it that.
		out = append(out, StratumScore{
			Stratum:   q,
			K:         kq,
			Precision: float64(hits) / float64(kq),
			Support:   support[q],
		})
	}
	return out
}
