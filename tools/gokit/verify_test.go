package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRunStepsFailFast(t *testing.T) {
	var ran []string
	newStep := func(id string, err error) step {
		return step{id: id, budget: time.Minute, run: func(context.Context) error {
			ran = append(ran, id)
			return err
		}}
	}
	err := runSteps(context.Background(), "test", []step{
		newStep("a", nil),
		newStep("b", errors.New("boom")),
		newStep("c", nil),
	})
	if err == nil || !strings.Contains(err.Error(), `"b"`) {
		t.Fatalf("runSteps() error = %v, want failure at step b", err)
	}
	if !slices.Equal(ran, []string{"a", "b"}) {
		t.Fatalf("ran = %v, want [a b] (fail-fast)", ran)
	}
}

func TestProfilesCompose(t *testing.T) {
	fast := stepIDs(fastSteps())
	full := stepIDs(fullSteps())
	ci := stepIDs(ciSteps())

	if !slices.Equal(full[:len(fast)], fast) {
		t.Errorf("full %v does not start with fast %v", full, fast)
	}
	for _, id := range []string{"build", "build-pg", "drift"} {
		if !slices.Contains(full, id) {
			t.Errorf("full profile missing step %q", id)
		}
	}
	if !slices.Equal(ci, full) {
		t.Errorf("ci %v, want full %v", ci, full)
	}
}

func stepIDs(steps []step) []string {
	ids := make([]string, 0, len(steps))
	for _, s := range steps {
		ids = append(ids, s.id)
	}
	return ids
}
