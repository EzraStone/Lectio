// Package retry computes backoff delays.
package retry

import (
	"time"

	"example.com/sample"
)

// Backoff returns the delay before attempt n.
func Backoff(spec string, n int) (time.Duration, error) {
	iv, err := sample.Parse(spec)
	if err != nil {
		return 0, err
	}
	d := iv.Every
	for i := 0; i < n; i++ {
		d *= 2
	}
	return d, nil
}

// Requeue reports whether another attempt should be made.
func Requeue(spec string, n int) bool {
	d, err := Backoff(spec, n)
	return err == nil && d < time.Hour
}

// Stack is a generic helper, present so the extractor's handling of generics
// is exercised: every instantiation must normalize to one symbol.
type Stack[T any] struct{ items []T }

// Push adds an item.
func (s *Stack[T]) Push(v T) { s.items = append(s.items, v) }

// Len reports the item count.
func (s *Stack[T]) Len() int { return len(s.items) }

// countAll is a generic function with two instantiations below.
func countAll[T any](xs []T) int { return len(xs) }

// Totals exercises two instantiations of countAll.
func Totals() int {
	return countAll([]int{1, 2}) + countAll([]string{"a"})
}
