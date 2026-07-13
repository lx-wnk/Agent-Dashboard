package pipeline

import (
	"context"
	"testing"
	"time"
)

// fakeConfigRepo is a minimal repo.PipelineConfigRepo stub that counts reads
// and returns a canned value, so tests can assert cache hit/miss behavior
// without a DB round trip.
type fakeConfigRepo struct {
	numberCalls int
	numberValue float64
	stringCalls int
	stringValue string
}

func (f *fakeConfigRepo) GetNumber(_ context.Context, _ string, fallback float64) float64 {
	f.numberCalls++
	if f.numberValue != 0 {
		return f.numberValue
	}
	return fallback
}

func (f *fakeConfigRepo) GetString(_ context.Context, _ string, fallback string) string {
	f.stringCalls++
	if f.stringValue != "" {
		return f.stringValue
	}
	return fallback
}

func (f *fakeConfigRepo) GetStringScoped(_ context.Context, _ *string, _, fallback string) string {
	return fallback
}

func (f *fakeConfigRepo) Set(_ context.Context, _, _ string) error { return nil }

func (f *fakeConfigRepo) SetScoped(_ context.Context, _ *string, _, _ string) error { return nil }

func (f *fakeConfigRepo) Delete(_ context.Context, _ string) error { return nil }

func (f *fakeConfigRepo) DeleteScoped(_ context.Context, _ *string, _ string) error { return nil }

func (f *fakeConfigRepo) GetAll(_ context.Context) (map[string]string, error) { return nil, nil }

func (f *fakeConfigRepo) GetStringForScope(_ context.Context, _ *string, _, fallback string) string {
	return fallback
}

func (f *fakeConfigRepo) GetAllScoped(_ context.Context, _ *string) (map[string]string, error) {
	return nil, nil
}

func TestConfigCache_Number_HitWithinTTL(t *testing.T) {
	cfgRepo := &fakeConfigRepo{numberValue: 5}
	c := newConfigCache(cfgRepo)
	ctx := context.Background()

	first := c.Number(ctx, "maxParallelOrchestrators", 3)
	second := c.Number(ctx, "maxParallelOrchestrators", 3)

	if first != 5 || second != 5 {
		t.Fatalf("got first=%d second=%d, want both 5", first, second)
	}
	if cfgRepo.numberCalls != 1 {
		t.Fatalf("cfgRepo.GetNumber called %d times, want 1 (second read should hit cache)", cfgRepo.numberCalls)
	}
}

func TestConfigCache_Number_MissFetchesAndStores(t *testing.T) {
	cfgRepo := &fakeConfigRepo{}
	c := newConfigCache(cfgRepo)
	ctx := context.Background()

	got := c.Number(ctx, "stageTimeoutSeconds", 42)

	if got != 42 {
		t.Fatalf("got %d, want fallback 42", got)
	}
	if cfgRepo.numberCalls != 1 {
		t.Fatalf("cfgRepo.GetNumber called %d times, want 1", cfgRepo.numberCalls)
	}
	if _, ok := c.m.Load("stageTimeoutSeconds"); !ok {
		t.Fatal("value was not stored in cache after miss")
	}
}

func TestConfigCache_Number_ExpiryRefetches(t *testing.T) {
	cfgRepo := &fakeConfigRepo{numberValue: 7}
	c := newConfigCache(cfgRepo)
	ctx := context.Background()

	c.Number(ctx, "maxAutoRetries", 3)
	// Force expiry by backdating the stored entry.
	c.m.Store("maxAutoRetries", cachedConfig{value: 7, expiresAt: time.Now().Add(-time.Second)})

	cfgRepo.numberValue = 9
	got := c.Number(ctx, "maxAutoRetries", 3)

	if got != 9 {
		t.Fatalf("got %d, want refetched value 9", got)
	}
	if cfgRepo.numberCalls != 2 {
		t.Fatalf("cfgRepo.GetNumber called %d times, want 2 (initial + refetch after expiry)", cfgRepo.numberCalls)
	}
}

func TestConfigCache_String_HitAndMiss(t *testing.T) {
	cfgRepo := &fakeConfigRepo{stringValue: "claude-opus-4-6"}
	c := newConfigCache(cfgRepo)
	ctx := context.Background()

	first := c.String(ctx, "stageModel.implementation", "fallback-model")
	second := c.String(ctx, "stageModel.implementation", "fallback-model")

	if first != "claude-opus-4-6" || second != "claude-opus-4-6" {
		t.Fatalf("got first=%q second=%q, want both claude-opus-4-6", first, second)
	}
	if cfgRepo.stringCalls != 1 {
		t.Fatalf("cfgRepo.GetString called %d times, want 1 (second read should hit cache)", cfgRepo.stringCalls)
	}
}

func TestConfigCache_Invalidate_ClearsEntries(t *testing.T) {
	cfgRepo := &fakeConfigRepo{numberValue: 1, stringValue: "v1"}
	c := newConfigCache(cfgRepo)
	ctx := context.Background()

	c.Number(ctx, "maxParallelOrchestrators", 3)
	c.String(ctx, "stageModel.implementation", "fallback")
	c.Invalidate()

	if _, ok := c.m.Load("maxParallelOrchestrators"); ok {
		t.Fatal("numeric entry still present after Invalidate")
	}
	if _, ok := c.m.Load("stageModel.implementation"); ok {
		t.Fatal("string entry still present after Invalidate")
	}

	cfgRepo.numberValue = 2
	got := c.Number(ctx, "maxParallelOrchestrators", 3)
	if got != 2 {
		t.Fatalf("got %d after invalidate+refetch, want refreshed value 2", got)
	}
	if cfgRepo.numberCalls != 2 {
		t.Fatalf("cfgRepo.GetNumber called %d times, want 2 (initial + refetch after Invalidate)", cfgRepo.numberCalls)
	}
}
