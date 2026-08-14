// Package billing computes billing cycles.
package billing

import (
	"time"

	"example.com/sample"
)

// Cycle returns the length of one billing cycle for a spec.
func Cycle(spec string) (time.Duration, error) {
	iv, err := sample.Parse(spec)
	if err != nil {
		return 0, err
	}
	return iv.Every, nil
}

// Draft renders an invoice line for a cycle.
func Draft(spec string) (string, error) {
	d, err := Cycle(spec)
	if err != nil {
		return "", err
	}
	return "billed every " + d.String(), nil
}
