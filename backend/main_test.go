package main

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

func TestKeepLoadingBackoff(t *testing.T) {
	var waits []time.Duration
	calls := 0
	load := func(context.Context) error {
		if calls++; calls < 8 {
			return errors.New("indisponible")
		}
		return nil
	}
	wait := func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	keepLoading(context.Background(), load, wait)
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour}
	if calls != 8 || !slices.Equal(waits, want) {
		t.Errorf("%d appels, attentes %v", calls, waits)
	}
	// Arrêt : l'attente interrompue met fin aux essais.
	calls = 0
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	keepLoading(ctx, load, sleepCtx)
	if calls != 0 {
		t.Errorf("%d appels après l'arrêt", calls)
	}
}
