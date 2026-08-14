package rank

import "testing"

func TestOrphaningFindsCodeWithNobodyLeftToAsk(t *testing.T) {
	long := refNow.AddDate(0, 0, -200) // well past the 90-day threshold
	recent := refNow.AddDate(0, 0, -5)

	b := newView().sym("pkg.abandoned", "abandoned.go").sym("pkg.owned", "owned.go").
		authorship("abandoned.go", "departed@example.com", 400, long).
		authorship("owned.go", "present@example.com", 400, recent)

	got := Orphaning{}.Compute(b.build(), params())
	if got["pkg.abandoned"] <= 0 {
		t.Error("code by a departed author scored nothing")
	}
	if got["pkg.owned"] != 0 {
		t.Errorf("code by an active author scored %v, want 0", got["pkg.owned"])
	}
}

func TestOrphaningWeighsShareAndVolume(t *testing.T) {
	long := refNow.AddDate(0, 0, -200)
	recent := refNow.AddDate(0, 0, -5)

	// Same 100% orphaned share, very different amounts of code.
	small := newView().sym("pkg.a", "a.go").authorship("a.go", "gone@example.com", 12, long)
	large := newView().sym("pkg.a", "a.go").authorship("a.go", "gone@example.com", 2000, long)

	s := Orphaning{}.Compute(small.build(), params())["pkg.a"]
	l := Orphaning{}.Compute(large.build(), params())["pkg.a"]
	if l <= s {
		t.Errorf("2000 orphaned lines (%v) should outrank 12 (%v)", l, s)
	}

	// Same volume, different share.
	mixed := newView().sym("pkg.a", "a.go").
		authorship("a.go", "gone@example.com", 500, long).
		authorship("a.go", "here@example.com", 1500, recent)
	whole := newView().sym("pkg.a", "a.go").
		authorship("a.go", "gone@example.com", 500, long)

	m := Orphaning{}.Compute(mixed.build(), params())["pkg.a"]
	w := Orphaning{}.Compute(whole.build(), params())["pkg.a"]
	if w <= m {
		t.Errorf("fully orphaned (%v) should outrank a quarter orphaned (%v)", w, m)
	}
}

func TestOrphanedShareForRationale(t *testing.T) {
	long := refNow.AddDate(0, 0, -200)
	recent := refNow.AddDate(0, 0, -5)

	b := newView().sym("pkg.a", "a.go").
		authorship("a.go", "gone@example.com", 300, long).
		authorship("a.go", "here@example.com", 100, recent)

	got := OrphanedShare(b.build(), refNow)
	if want := 0.75; got["a.go"] != want {
		t.Errorf("orphaned share = %v, want %v", got["a.go"], want)
	}
}

func TestOrphaningWithNoAuthorshipData(t *testing.T) {
	b := newView().sym("pkg.a", "a.go")
	if got := (Orphaning{}).Compute(b.build(), params()); len(got) != 0 {
		t.Errorf("no authorship data should yield no scores, got %v", got)
	}
}

// ------------------------------------------------------------- ai density --

func TestAIDensityScoresMachineAuthoredFiles(t *testing.T) {
	b := newView().sym("pkg.generated", "generated.go").sym("pkg.handwritten", "handwritten.go")

	for i := 0; i < 4; i++ {
		b.aiCommit(refNow.AddDate(0, 0, -i*3), "add feature", "generated.go")
	}
	for i := 0; i < 4; i++ {
		b.commit(refNow.AddDate(0, 0, -i*3), "add feature", "dev@example.com", "handwritten.go")
	}

	got := AIDensity{}.Compute(b.build(), params())
	if got["pkg.generated"] <= 0 {
		t.Error("machine-authored file scored nothing")
	}
	if got["pkg.handwritten"] != 0 {
		t.Errorf("hand-written file scored %v, want 0", got["pkg.handwritten"])
	}
}

// Absence of markers means "this repo does not record it", not "all of this
// was written by hand". Scoring zero everywhere is the honest output; scoring
// low would assert a fact we do not have.
func TestAIDensityStaysSilentWithoutMarkers(t *testing.T) {
	b := newView().sym("pkg.a", "a.go")
	for i := 0; i < 10; i++ {
		b.commit(refNow.AddDate(0, 0, -i), "work", "dev@example.com", "a.go")
	}
	v := b.build()

	if got := (AIDensity{}).Compute(v, params()); len(got) != 0 {
		t.Errorf("a repo with no AI markers produced scores: %v", got)
	}
	if HasAIMarkers(v) {
		t.Error("HasAIMarkers = true on a repo with no marked commits")
	}
}

func TestAIDensityUsesLineCountsNotCommitCounts(t *testing.T) {
	// One AI commit adding 400 lines vs one adding 10: the commit count cannot
	// tell them apart, the line count can.
	big := newView().sym("pkg.a", "a.go")
	big.aiCommit(refNow, "generate", "a.go")
	big.v.Commits[0].Files[0].Added = 400

	small := newView().sym("pkg.a", "a.go")
	small.aiCommit(refNow, "generate", "a.go")
	small.v.Commits[0].Files[0].Added = 10

	bs := AIDensity{}.Compute(big.build(), params())["pkg.a"]
	ss := AIDensity{}.Compute(small.build(), params())["pkg.a"]
	if bs <= ss {
		t.Errorf("400 generated lines (%v) should outrank 10 (%v)", bs, ss)
	}
}

func TestHasAIMarkers(t *testing.T) {
	b := newView().sym("pkg.a", "a.go")
	b.aiCommit(refNow, "generate", "a.go")
	if !HasAIMarkers(b.build()) {
		t.Error("HasAIMarkers = false despite a marked commit")
	}
}
