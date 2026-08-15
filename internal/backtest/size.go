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
	// Spread is the median within-band size ratio. See StratumScore.Spread.
	Spread float64
}

// Won reports whether lectio outscored the baseline in this band.
func (b BandComparison) Won() bool { return b.Lectio > b.Largest }

// TightSpread is the within-band size ratio below which a band has actually
// controlled for size.
//
// Bands are quartiles by file count, which does not equalize size within one.
// On the corpus, Q2 and Q3 span 2x to 4x while Q1 spans 10x to 39x — a size
// strategy still carries real size information inside a band that loose, so a
// result there says much less about choices than the same result in a tight
// band. Four is where the corpus separates the two groups.
const TightSpread = 4.0

// Tight reports whether size is controlled well enough in this band for the
// comparison to be about choices rather than about size.
func (b BandComparison) Tight() bool { return b.Spread > 0 && b.Spread <= TightSpread }

// SizeReading interprets the stratified table, which exists to settle one
// question: when largest-files wins, is it choosing better files or just
// bigger ones?
type SizeReading struct {
	// Incumbent names the size strategy being compared against, which differs
	// by granularity.
	Incumbent      string
	OverallLectio  float64
	OverallLargest float64
	Bands          []BandComparison
	// LectioWins counts bands where lectio scored higher.
	LectioWins int
	// TightBands counts bands where size is controlled well enough for the
	// comparison to be about choices. See BandComparison.Tight.
	TightBands int
	// TightGap is the mean lectio-minus-largest difference across those tight
	// bands. Near zero means the ranking is adding nothing over size — which
	// is a different finding from being worse at choosing.
	TightGap float64
	// MeanGap is the same difference across every comparable band.
	//
	// Band counts alone misreport lopsided results: winning one band by 0.7
	// points and losing three by 3.6, 6.6 and 10.0 is not "mixed", and the
	// first version of this reading said it was.
	MeanGap float64
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
	// Whichever size strategy is the one to beat at this granularity. On a
	// symbolic run that is largest-symbols: comparing against largest-files
	// there produces a reassuring line about a strategy nobody is worried
	// about, while the one that actually outscored lectio goes unmentioned.
	incumbent := "largest files"
	if rep.Target.Symbolic() {
		incumbent = LargestSymbols{}.Name()
	}

	var r SizeReading
	r.Incumbent = incumbent
	for _, a := range rep.Aggregates {
		switch a.Strategy {
		case DefaultVariant:
			r.OverallLectio = a.PrecisionA
		case incumbent:
			r.OverallLargest = a.PrecisionA
		}
	}

	type cell struct {
		p      float64
		n      int
		spread float64
	}
	lectio := map[int]cell{}
	largest := map[int]cell{}
	for _, s := range rep.Strata {
		switch s.Strategy {
		case DefaultVariant:
			lectio[s.Stratum] = cell{s.Precision, s.Cases, s.Spread}
		case incumbent:
			largest[s.Stratum] = cell{s.Precision, s.Cases, s.Spread}
		}
	}

	var tightSum float64
	for q := 0; q < NumStrata; q++ {
		l, okL := lectio[q]
		b, okB := largest[q]
		if !okL || !okB {
			continue
		}
		cmp := BandComparison{
			Stratum: q, Label: StratumLabels[q],
			Lectio: l.p, Largest: b.p, Cases: min(l.n, b.n), Spread: l.spread,
		}
		r.Bands = append(r.Bands, cmp)
		if cmp.Won() {
			r.LectioWins++
		}
		if cmp.Tight() {
			r.TightBands++
			tightSum += cmp.Lectio - cmp.Largest
		}
	}
	if r.TightBands > 0 {
		r.TightGap = tightSum / float64(r.TightBands)
	}
	if n := len(r.Bands); n > 0 {
		var sum float64
		for _, b := range r.Bands {
			sum += b.Lectio - b.Largest
		}
		r.MeanGap = sum / float64(n)
	}

	r.Note = readingNote(r)
	return r
}

// negligibleGap is the difference below which two strategies are the same
// strategy as far as this corpus can tell. Matches the threshold the ablation
// table is read at.
const negligibleGap = 0.015

// decisiveGap is the mean per-band difference above which a split band count
// stops being the interesting fact.
//
// Set at three times negligibleGap: if the average band gap is more than twice
// what this corpus can even resolve, the direction is not in doubt however the
// individual bands fell. Deliberately symmetric — it makes the reading more
// skeptical of a narrow lectio win too, not only of a narrow lectio loss.
const decisiveGap = 0.045

func readingNote(r SizeReading) string {
	n := len(r.Bands)
	switch {
	case n == 0:
		return "no size band had enough files and enough ground truth to score"

	case r.OverallLectio > r.OverallLargest:
		return fmt.Sprintf("lectio leads overall and in %d of %d size bands", r.LectioWins, n)

	case r.LectioWins == n:
		return fmt.Sprintf(
			"%s leads overall while losing all %d size bands — the metric is "+
				"rewarding size, not better choices", r.Incumbent, n)

	// Losing every band, but by nothing once size is held tight. The ranking
	// is not making worse choices; it is making the same ones, which is the
	// more precise and less flattering finding of the two.
	case r.LectioWins == 0 && r.TightBands > 0 && -r.TightGap < negligibleGap:
		return fmt.Sprintf(
			"%s leads every band, but by %.1f pp across the %d bands where size "+
				"is actually controlled — the ranking is not losing on choices, it is adding "+
				"nothing over size",
			r.Incumbent, -r.TightGap*100, r.TightBands)

	case r.LectioWins == 0:
		return fmt.Sprintf(
			"%s leads in all %d size bands — its advantage is not an artifact "+
				"of the metric", r.Incumbent, n)

	// Split bands, but lopsided ones. Counting alone would call a result mixed
	// when the wins are fractions of a point and the losses are many.
	case r.MeanGap <= -decisiveGap:
		return fmt.Sprintf(
			"%s leads overall and by %.1f pp averaged across bands; lectio takes %d of %d "+
				"and only narrowly — the split is not the story, the margin is",
			r.Incumbent, -r.MeanGap*100, r.LectioWins, n)

	default:
		return fmt.Sprintf(
			"%s leads overall; lectio wins %d of %d size bands — mixed, and not "+
				"enough to settle it", r.Incumbent, r.LectioWins, n)
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
	// Spread is the largest file in this band divided by the smallest, and it
	// is the caveat the table has to carry.
	//
	// Quartiles by count do not equalize size within a band. Measured on the
	// corpus, Q1 typically spans 10x to 39x while Q2 and Q3 span 2x to 4x — so
	// a size strategy still holds real size information inside Q1, and a win
	// there is much weaker evidence of better choices than the same win in Q2.
	// Reporting the number beats hoping the reader assumes the bands are tight.
	Spread float64
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

	// Candidate counts and size extremes per band, over everything the
	// strategy could have named.
	candidates := make([]int, NumStrata)
	lo := make([]int, NumStrata)
	hi := make([]int, NumStrata)
	for f, q := range bands {
		candidates[q]++
		if lo[q] == 0 || sizes[f] < lo[q] {
			lo[q] = sizes[f]
		}
		if sizes[f] > hi[q] {
			hi[q] = sizes[f]
		}
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
		spread := 1.0
		if lo[q] > 0 {
			spread = float64(hi[q]) / float64(lo[q])
		}
		out = append(out, StratumScore{
			Stratum:   q,
			K:         kq,
			Precision: float64(hits) / float64(kq),
			Support:   support[q],
			Spread:    spread,
		})
	}
	return out
}
