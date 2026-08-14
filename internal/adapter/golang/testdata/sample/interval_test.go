package sample

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	iv, err := Parse("30s")
	if err != nil || iv.Every != 30*time.Second {
		t.Fatalf("Parse(30s) = %+v, %v", iv, err)
	}
}

func TestParseJitter(t *testing.T) {
	iv, err := Parse("5m+1s")
	if err != nil || iv.Jitter != time.Second {
		t.Fatalf("Parse(5m+1s) = %+v, %v", iv, err)
	}
}

func TestParseEmpty(t *testing.T) {
	iv, err := Parse("")
	if err != nil || iv.Every != DefaultInterval {
		t.Fatalf("Parse(\"\") = %+v, %v", iv, err)
	}
}
