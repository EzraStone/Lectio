package graph

import (
	"reflect"
	"testing"
)

// The graph from the spec's hero figure: parseInterval with three direct
// dependents and five more one hop further out.
func specGraph() *Graph {
	return build([][2]string{
		{"scheduler.Next", "parseInterval"},
		{"billing.Cycle", "parseInterval"},
		{"retry.Backoff", "parseInterval"},
		{"cron.Register", "scheduler.Next"},
		{"digest.Send", "scheduler.Next"},
		{"invoice.Draft", "billing.Cycle"},
		{"queue.Requeue", "retry.Backoff"},
		{"webhook.Retry", "retry.Backoff"},
	})
}

func TestDependentsFindsTheBlastRadius(t *testing.T) {
	got := Dependents(specGraph(), "parseInterval", 0)
	want := map[string]int{
		"scheduler.Next": 1,
		"billing.Cycle":  1,
		"retry.Backoff":  1,
		"cron.Register":  2,
		"digest.Send":    2,
		"invoice.Draft":  2,
		"queue.Requeue":  2,
		"webhook.Retry":  2,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Dependents(parseInterval) = %v, want %v", got, want)
	}
}

func TestDependentsExcludesTheSeed(t *testing.T) {
	if _, ok := Dependents(specGraph(), "parseInterval", 0)["parseInterval"]; ok {
		t.Error("the seed must not appear in its own blast radius")
	}
}

func TestDependentsRespectsMaxDepth(t *testing.T) {
	got := Dependents(specGraph(), "parseInterval", 1)
	want := map[string]int{"scheduler.Next": 1, "billing.Cycle": 1, "retry.Backoff": 1}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("depth-1 dependents = %v, want %v", got, want)
	}
}

func TestDependenciesWalksForward(t *testing.T) {
	got := Dependencies(specGraph(), "cron.Register", 0)
	want := map[string]int{"scheduler.Next": 1, "parseInterval": 2}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Dependencies(cron.Register) = %v, want %v", got, want)
	}
}

func TestDependentsOfUnknownSymbol(t *testing.T) {
	if got := Dependents(specGraph(), "nope", 0); len(got) != 0 {
		t.Errorf("unknown symbol should yield an empty set, got %v", got)
	}
}

func TestReachableHandlesCycles(t *testing.T) {
	g := build([][2]string{{"a", "b"}, {"b", "c"}, {"c", "a"}})
	got := Dependencies(g, "a", 0)
	want := map[string]int{"b": 1, "c": 2}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cycle walk = %v, want %v", got, want)
	}
}

func TestProximityIgnoresDirection(t *testing.T) {
	g := specGraph()
	dist := Proximity(g, []string{"parseInterval"})

	at := func(id string) int {
		i, ok := g.Index(id)
		if !ok {
			t.Fatalf("%s missing", id)
		}
		return dist[i]
	}

	if at("parseInterval") != 0 {
		t.Errorf("seed distance = %d, want 0", at("parseInterval"))
	}
	// Callers are neighbors even though every edge points away from them.
	if at("scheduler.Next") != 1 {
		t.Errorf("scheduler.Next distance = %d, want 1", at("scheduler.Next"))
	}
	if at("webhook.Retry") != 2 {
		t.Errorf("webhook.Retry distance = %d, want 2", at("webhook.Retry"))
	}
}

func TestProximityMarksUnreachable(t *testing.T) {
	g := build([][2]string{{"a", "b"}, {"island", "islet"}})
	dist := Proximity(g, []string{"a"})
	i, _ := g.Index("island")
	if dist[i] != -1 {
		t.Errorf("disconnected node distance = %d, want -1", dist[i])
	}
}
