package backtest

import (
	"fmt"
	"testing"
)

// couplingRepos builds n repositories whose fix rate is `mult` times their
// base rate, so the pooled lift is known before the test runs.
func couplingRepos(n int, base, mult float64) []RepoCoupling {
	out := make([]RepoCoupling, 0, n)
	for i := 0; i < n; i++ {
		fixes := 100
		onCoupled := int(base * mult * float64(fixes))
		out = append(out, RepoCoupling{
			Repo: fmt.Sprintf("repo%02d", i),
			CouplingResult: CouplingResult{
				Pairs: 50, CoupledFiles: 20,
				NewcomerFixes: fixes, FixesOnCoupled: onCoupled,
				BaseRate: base,
				FixRate:  float64(onCoupled) / float64(fixes),
				Lift:     mult,
			},
		})
	}
	return out
}

func TestBootstrapCouplingBracketsThePooledLift(t *testing.T) {
	rs := couplingRepos(20, 0.5, 1.2)
	iv := BootstrapCoupling(rs, DefaultLevel, 2000, 3)

	closeTo(t, iv.Point, PooledCoupling(rs).Lift, 1e-12, "point estimate")
	if iv.Lo > iv.Point || iv.Hi < iv.Point {
		t.Errorf("[%.4f, %.4f] does not bracket %.4f", iv.Lo, iv.Hi, iv.Point)
	}
	if iv.Repos != 20 {
		t.Errorf("resampled %d repositories, want 20", iv.Repos)
	}
}

func TestBootstrapCouplingIsDeterministic(t *testing.T) {
	rs := couplingRepos(15, 0.4, 1.1)
	first := BootstrapCoupling(rs, DefaultLevel, 1000, 11)
	for i := 0; i < 4; i++ {
		if got := BootstrapCoupling(rs, DefaultLevel, 1000, 11); got != first {
			t.Fatalf("same seed gave %+v then %+v", first, got)
		}
	}
}

// A corpus of identical repositories has no between-repository variance, so
// the interval collapses onto the point. Anything wider would be invented.
func TestBootstrapCouplingOnIdenticalRepositoriesIsTight(t *testing.T) {
	iv := BootstrapCoupling(couplingRepos(12, 0.5, 1.4), DefaultLevel, 2000, 5)
	if iv.Hi-iv.Lo > 0.01 {
		t.Errorf("identical repositories gave a %.4f-wide interval", iv.Hi-iv.Lo)
	}
	closeTo(t, iv.Point, 1.4, 0.02, "pooled lift")
}

// A corpus that disagrees with itself must produce an interval wide enough to
// contain no relationship, however far the pooled point sits from it.
func TestBootstrapCouplingWidensWhenRepositoriesDisagree(t *testing.T) {
	var rs []RepoCoupling
	rs = append(rs, couplingRepos(6, 0.3, 2.5)...)
	for i, r := range couplingRepos(6, 0.8, 0.6) {
		r.Repo = fmt.Sprintf("low%02d", i)
		rs = append(rs, r)
	}
	iv := BootstrapCoupling(rs, DefaultLevel, 2000, 7)

	if iv.Hi-iv.Lo < 0.2 {
		t.Errorf("repositories at 2.5x and 0.6x gave a %.4f-wide interval", iv.Hi-iv.Lo)
	}
	if iv.ExcludesNoRelationship() {
		t.Errorf("[%.3f, %.3f] claimed a relationship from a corpus that disagrees with itself",
			iv.Lo, iv.Hi)
	}
}

// The same floor every other interval here applies. Below it a percentile
// bootstrap draws from too few distinct resamples to mean anything.
func TestBootstrapCouplingRefusesBelowTheClusterFloor(t *testing.T) {
	for n := 1; n < MinClusters; n++ {
		iv := BootstrapCoupling(couplingRepos(n, 0.4, 1.5), DefaultLevel, 2000, 2)
		if iv.Lo != 0 || iv.Hi != 0 {
			t.Errorf("%d repositories produced [%.3f, %.3f]", n, iv.Lo, iv.Hi)
		}
		if iv.ExcludesNoRelationship() {
			t.Errorf("%d repositories claimed a relationship", n)
		}
		if iv.Point == 0 {
			t.Errorf("%d repositories lost the point estimate too", n)
		}
	}
}

// Repositories the sample-size guards declined contributed no lift, and
// resampling them would put mass on a value PooledCoupling never used.
func TestBootstrapCouplingSkipsGuardedRepositories(t *testing.T) {
	rs := couplingRepos(10, 0.5, 1.2)
	rs = append(rs, RepoCoupling{
		Repo: "too-thin",
		CouplingResult: CouplingResult{
			NewcomerFixes: 3, FixesOnCoupled: 2, BaseRate: 0.1, FixRate: 0.667, Lift: 6.67,
		},
	})
	if got := BootstrapCoupling(rs, DefaultLevel, 1000, 4).Repos; got != 10 {
		t.Errorf("resampled %d repositories, want the 10 that cleared the guards", got)
	}
}

func TestExcludesNoRelationshipReadsOne(t *testing.T) {
	for _, tc := range []struct {
		lo, hi float64
		want   bool
	}{
		{1.10, 1.50, true},  // clear above
		{0.60, 0.90, true},  // clear below
		{0.90, 1.20, false}, // straddles
		{1.00, 1.40, false}, // touches exactly
	} {
		iv := CouplingInterval{Lo: tc.lo, Hi: tc.hi, Repos: 20}
		if got := iv.ExcludesNoRelationship(); got != tc.want {
			t.Errorf("[%.2f, %.2f] = %v, want %v", tc.lo, tc.hi, got, tc.want)
		}
	}
	if (CouplingInterval{Lo: 1.1, Hi: 1.5}).ExcludesNoRelationship() {
		t.Error("an interval from no repositories claimed a relationship")
	}
}

// The question a null result has to answer: was the interval tight enough to
// have excluded anything worth having?
func TestCouplingPowerReportsWhatCouldHaveBeenSeen(t *testing.T) {
	tight := CouplingInterval{Point: 1.02, Lo: 0.94, Hi: 1.11, Repos: 24}
	closeTo(t, CouplingPower(tight), 0.11, 1e-9, "power of a tight null")

	loose := CouplingInterval{Point: 1.02, Lo: 0.50, Hi: 2.00, Repos: 24}
	closeTo(t, CouplingPower(loose), 1.00, 1e-9, "power of a loose null")

	if CouplingPower(loose) <= CouplingPower(tight) {
		t.Error("a loose interval reported at least as much power as a tight one")
	}
	if got := CouplingPower(CouplingInterval{}); got != 0 {
		t.Errorf("power of an absent interval = %v", got)
	}
}
