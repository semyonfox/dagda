package main

import (
	"testing"

	"github.com/semyonfox/dagda/engine/internal/pricing"
)

func TestMergePreservesProviderHistory(t *testing.T) {
	old := pricing.Rate{From: pricing.MustParseDate("2026-01-01"), In: 1, Out: 2, TierCacheRead: 0.1}
	fresh := pricing.Rate{From: pricing.MustParseDate("2026-08-01"), In: 1, Out: 2, TierCacheRead: 0.2}
	prev := &dataset{Models: map[string]*pricing.Model{}, ByProvider: map[string]*pricing.Model{
		"p\x00m":    {ID: "m", Rates: []pricing.Rate{old}},
		"p\x00gone": {ID: "gone", Rates: []pricing.Rate{old}},
	}}
	next := &dataset{Models: map[string]*pricing.Model{}, ByProvider: map[string]*pricing.Model{
		"p\x00m": {ID: "m", Rates: []pricing.Rate{fresh}},
	}}

	merge(prev, next, pricing.MustParseDate("2026-08-28"))
	if got := len(next.ByProvider["p\x00m"].Rates); got != 2 {
		t.Fatalf("changed provider history has %d rates, want 2", got)
	}
	if _, ok := next.ByProvider["p\x00gone"]; !ok {
		t.Error("vanished provider history was discarded")
	}
}

func TestMissingOutputIsNotFree(t *testing.T) {
	input := 1.0
	if _, ok := toRate(mdCost{Input: &input}, "test", "test", pricing.MustParseDate("2026-09-07")); ok {
		t.Fatal("missing output rate accepted")
	}
}

func TestPaidToZeroRequiresReview(t *testing.T) {
	date := pricing.MustParseDate("2026-09-07")
	previous := &dataset{Models: map[string]*pricing.Model{"test": {ID: "test", Rates: []pricing.Rate{{From: date, In: 1, Out: 2}}}}}
	next := &dataset{Models: map[string]*pricing.Model{"test": {ID: "test", Rates: []pricing.Rate{{From: date}}}}}
	merge(previous, next, date)
	if next.Models["test"].Rates[0].In != 1 {
		t.Fatal("paid price silently replaced with zero")
	}
}

func TestSyncPreservesSchedules(t *testing.T) {
	old := &dataset{Schedules: map[string]pricing.Schedule{"peak": {Days: []int{1}}}, Aliases: map[string]string{"old-name": "model"}}
	next := &dataset{}
	merge(old, next, pricing.MustParseDate("2026-09-07"))
	if len(next.Schedules["peak"].Days) != 1 || next.Aliases["old-name"] != "model" {
		t.Fatal("lost historical pricing metadata")
	}
}

func TestAliasGenerationDeterministic(t *testing.T) {
	for range 100 {
		ds := &dataset{Models: map[string]*pricing.Model{"openai.gpt-5.4": {}, "openai-gpt-5.4": {}}, Aliases: map[string]string{}}
		buildAliases(ds)
		if ds.Aliases["openai-gpt-5-4"] != "openai-gpt-5.4" {
			t.Fatal("unstable alias target")
		}
	}
}

func TestSyncKeepsSupplementalRatesAndLocalModelAliases(t *testing.T) {
	id := "qwen2.5-coder-3b-instruct"
	old := &dataset{
		Models:  map[string]*pricing.Model{id: {ID: id, Rates: []pricing.Rate{{From: pricing.MustParseDate("2026-10-07"), In: 0.01, Out: 0.032, Source: "codelace"}}}},
		Aliases: map[string]string{"qwen2.5-coder:3b": id},
	}
	next := &dataset{Models: map[string]*pricing.Model{"qwen2-5-coder-7b-instruct": {}}, Aliases: map[string]string{}}
	buildAliases(next)
	merge(old, next, pricing.MustParseDate("2026-10-08"))
	for query, canonical := range map[string]string{"qwen2.5-coder:3b": id, "qwen2.5-coder:7b": "qwen2-5-coder-7b-instruct"} {
		if next.Aliases[query] != canonical {
			t.Fatalf("alias %s = %s, want %s", query, next.Aliases[query], canonical)
		}
	}
	if r := next.Models[id].Rates[0]; r.From != pricing.MustParseDate("2026-10-07") || r.In != 0.01 || r.Out != 0.032 || r.Source != "codelace" {
		t.Fatalf("supplemental price history changed: %+v", r)
	}
}

func ptr(v float64) *float64 { return &v }

// grok-code-fast-1 was billed with free cache reads because the alphabetically
// first reseller left the field out while others published $0.02.
func TestResellerWithCachePriceWinsTie(t *testing.T) {
	providers := map[string]mdProvider{
		"abacus":   {Models: map[string]mdModel{"grok-code-fast-1": {Cost: &mdCost{Input: ptr(0.2), Output: ptr(1.5)}}}},
		"helicone": {Models: map[string]mdModel{"grok-code-fast-1": {Cost: &mdCost{Input: ptr(0.2), Output: ptr(1.5), CacheRead: ptr(0.02)}}}},
	}
	r := build(providers, pricing.MustParseDate("2026-11-10")).Models["grok-code-fast-1"].Rates[0]
	if r.Source != "helicone" || r.CacheRead != 0.02 {
		t.Fatalf("picked %s with cache read %v, want helicone at 0.02", r.Source, r.CacheRead)
	}
}

func TestDatedClaudeSnapshotUsesFirstPartyBase(t *testing.T) {
	providers := map[string]mdProvider{
		"anthropic": {Models: map[string]mdModel{
			"claude-sonnet-4":           {Cost: &mdCost{Input: ptr(3), Output: ptr(15), CacheRead: ptr(0.3)}},
			"claude-haiku-4-5":          {Cost: &mdCost{Input: ptr(1), Output: ptr(5), CacheRead: ptr(0.1)}},
			"claude-haiku-4-5-20251001": {Cost: &mdCost{Input: ptr(1), Output: ptr(5), CacheRead: ptr(0.1)}},
		}},
		"abacus": {Models: map[string]mdModel{"claude-sonnet-4-20250514": {Cost: &mdCost{Input: ptr(3), Output: ptr(15)}}}},
	}
	ds := build(providers, pricing.MustParseDate("2026-11-10"))
	if got := ds.Aliases["claude-sonnet-4-20250514"]; got != "claude-sonnet-4" {
		t.Fatalf("reseller-only snapshot aliased to %q, want claude-sonnet-4", got)
	}
	if got, ok := ds.Aliases["claude-haiku-4-5-20251001"]; ok {
		t.Fatalf("first-party snapshot aliased to %q", got)
	}
}

func TestFirstPartyFileFillsGapsOnly(t *testing.T) {
	reseller := &pricing.Model{ID: "claude-mythos-5-2", Rates: []pricing.Rate{{In: 1, Out: 1, Source: "reseller"}}}
	upstream := &pricing.Model{ID: "claude-mythos-5-1", FirstParty: true, Rates: []pricing.Rate{{In: 9, Out: 45, Source: "anthropic"}}}
	ds := &dataset{
		Models:  map[string]*pricing.Model{"claude-mythos-5-2": reseller, "claude-mythos-5-1": upstream},
		Aliases: map[string]string{"gemini-4-carbon": "something-else"},
	}
	applyFirstParty(ds, map[string]bool{"claude-mythos-5-1": true})

	if r := ds.Models["claude-mythos-5-2"].Rates[0]; r.In != 8 || r.Out != 40 || r.CacheRead != 0.2 || r.CacheWrite != 10 || r.CacheWrite1h != 16 {
		t.Errorf("mythos 5.2 = %+v, want the Anthropic list rate 8/40, 0.20 read, 10 and 16 writes", r)
	}
	if ds.Models["claude-mythos-5-1"] != upstream {
		t.Error("hand-checked rate replaced an upstream first-party one")
	}
	if _, ok := ds.Aliases["gemini-4-carbon"]; ok {
		t.Error("an alias still shadows a hand-checked model")
	}
	if rates := ds.Models["gemini-4-carbon"].Rates; len(rates) != 2 || rates[1].From != pricing.MustParseDate("2026-12-01") || rates[1].In != 4 {
		t.Errorf("gemini-4-carbon lost its announced 1 December rise: %+v", rates)
	}
}

func TestNewlyPublishedCacheReadRepricesBacklog(t *testing.T) {
	first := pricing.MustParseDate("2026-07-14")
	prev := &dataset{Models: map[string]*pricing.Model{"m": {ID: "m", Rates: []pricing.Rate{{From: first, In: 0.2, Out: 1.5, CacheWrite: 0.2, Source: "abacus"}}}}}
	next := &dataset{Models: map[string]*pricing.Model{"m": {ID: "m", Rates: []pricing.Rate{{In: 0.2, Out: 1.5, CacheRead: 0.02, CacheWrite: 0.2, Source: "helicone"}}}}}
	merge(prev, next, pricing.MustParseDate("2026-11-10"))
	rates := next.Models["m"].Rates
	if len(rates) != 1 || rates[0].From != first || rates[0].CacheRead != 0.02 || rates[0].Source != "helicone" {
		t.Fatalf("rates = %+v, want the July rate with a 0.02 cache read from helicone", rates)
	}
}
