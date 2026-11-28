package cost

import (
	"math"
	"testing"
	"time"

	"github.com/semyonfox/dagda/engine/internal/model"
	"github.com/semyonfox/dagda/engine/internal/pricing"
)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		panic(err)
	}
	return t.Add(12 * time.Hour)
}

func table() *pricing.Table {
	return &pricing.Table{
		Updated: pricing.MustParseDate("2026-08-27"),
		Models: map[string]*pricing.Model{
			"m1": {ID: "m1", Rates: []pricing.Rate{{
				From: pricing.MustParseDate("2026-01-01"),
				In:   10, Out: 100, CacheRead: 1, CacheWrite: 12.5, CacheWrite1h: 20,
			}}},
		},
		ByProvider: map[string]*pricing.Model{},
		Aliases:    map[string]string{},
	}
}

func near(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("cost = %v, want %v", got, want)
	}
}

func TestPricesEachBucketAtItsOwnRate(t *testing.T) {
	c := Of(model.Turn{
		Model:     "m1",
		Timestamp: at("2026-06-01"),
		Usage: model.Usage{
			Input: 1_000_000, Output: 1_000_000,
			CacheRead: 1_000_000, CacheWrite: 1_000_000,
		},
	}, table())
	// 10 + 100 + 1 + 12.5
	near(t, c.USD, 123.5)
}

// The 1-hour cache TTL bills at a different rate from the 5-minute one, and the
// 1h tokens are a SUBSET of the cache-write total, not an extra bucket.
func TestCacheWrite1hIsASubsetPricedSeparately(t *testing.T) {
	c := Of(model.Turn{
		Model:     "m1",
		Timestamp: at("2026-06-01"),
		Usage:     model.Usage{CacheWrite: 1_000_000, CacheWrite1h: 400_000},
	}, table())
	// 600k at 12.5 + 400k at 20
	near(t, c.USD, 0.6*12.5+0.4*20)
}

// Batch traffic bills at half rate everywhere it is offered.
func TestBatchTierHalvesCost(t *testing.T) {
	base := model.Turn{Model: "m1", Timestamp: at("2026-06-01"), Usage: model.Usage{Output: 1_000_000}}
	std := Of(base, table())
	base.ServiceTier = "batch"
	batch := Of(base, table())
	near(t, std.USD, 100)
	near(t, batch.USD, 50)
}

// A priority call is priced from the vendor's own priority card, which is not
// a flat multiple of standard. Without a card, priority stays at standard.
func TestPriorityTierUsesItsOwnCard(t *testing.T) {
	tbl := table()
	tbl.Models["fast"] = &pricing.Model{ID: "fast", Rates: []pricing.Rate{{
		From: pricing.MustParseDate("2026-01-01"),
		In:   2, Out: 10, CacheRead: 0.2, CacheWrite: 2.5,
		TierThreshold: 272_000, TierIn: 4, TierOut: 15, TierCacheRead: 0.4, TierCacheWrite: 5,
		Priority: &pricing.SpeedRate{In: 4, Out: 25, CacheRead: 0.4, TierIn: 8},
	}}}
	call := model.Turn{Model: "fast", Timestamp: at("2026-06-01"), ServiceTier: "priority",
		Usage: model.Usage{Input: 100_000, Output: 100_000, CacheRead: 100_000, CacheWrite: 50_000}}
	// a 250k prompt, under the threshold: 0.4 + 2.5 + 0.04, and the blank
	// cache write scales with input, 2.5 * 4/2 = 5 per million, so 0.25
	near(t, Of(call, tbl).USD, 3.19)

	// a 3M prompt: tier input 8 is published, tier output scales with output
	// (15 * 25/10), tier cache read and write with input (0.4 * 2, 5 * 2)
	long := call
	long.Usage = model.Usage{Input: 1_000_000, Output: 1_000_000, CacheRead: 1_000_000, CacheWrite: 1_000_000}
	near(t, Of(long, tbl).USD, 8+37.5+0.8+10)

	std := call
	std.ServiceTier = ""
	// 0.2 + 1 + 0.02 + 0.125
	near(t, Of(std, tbl).USD, 1.345)

	noCard := model.Turn{Model: "m1", Timestamp: at("2026-06-01"), ServiceTier: "priority", Usage: model.Usage{Output: 1_000_000}}
	near(t, Of(noCard, tbl).USD, 100)
}

// Subscription-backed traffic is still priced at list. Zeroing it is what made
// the old headline total incomparable across agents.
func TestSubscriptionIsLabelledNotZeroed(t *testing.T) {
	c := Of(model.Turn{
		Model:     "m1",
		Agent:     model.AgentCodex,
		Endpoint:  "https://chatgpt.com/backend-api/codex",
		Timestamp: at("2026-06-01"),
		Usage:     model.Usage{Output: 1_000_000},
	}, table())
	if c.Billing != BillingSubscription {
		t.Errorf("billing = %v, want subscription", c.Billing)
	}
	near(t, c.USD, 100)
}

// Grok Build's persisted local ledger has no endpoint or credential-mode
// marker. Its normal product route is a flat plan, so retain the same useful
// list-price caveat as the other first-party coding agents.
func TestGrokDefaultsToSubscription(t *testing.T) {
	c := Of(model.Turn{
		Model:     "m1",
		Agent:     model.AgentGrok,
		Timestamp: at("2026-06-01"),
		Usage:     model.Usage{Output: 1_000_000},
	}, table())
	if c.Billing != BillingSubscription {
		t.Errorf("billing = %v, want subscription", c.Billing)
	}
	near(t, c.USD, 100)
}

func TestLocalIsLabelledNotZeroed(t *testing.T) {
	c := Of(model.Turn{
		Model:     "m1",
		Provider:  "ollama",
		Timestamp: at("2026-06-01"),
		Usage:     model.Usage{Output: 1_000_000},
	}, table())
	if c.Billing != BillingLocal {
		t.Errorf("billing = %v, want local", c.Billing)
	}
	near(t, c.USD, 100)
}

// An unpriced turn contributes nothing and says so. It must never look free.
func TestUnpricedTurnIsFlagged(t *testing.T) {
	c := Of(model.Turn{
		Model:     "no-such-model",
		Timestamp: at("2026-06-01"),
		Usage:     model.Usage{Output: 5_000_000},
	}, table())
	if c.Priced() {
		t.Fatal("unknown model reported as priced")
	}
	near(t, c.USD, 0)
}

func TestAliasReportsItsCanonicalModel(t *testing.T) {
	tbl := table()
	tbl.Aliases["masked-model"] = "m1"
	c := Of(model.Turn{
		Model:     "masked-model",
		Timestamp: at("2026-06-01"),
		Usage:     model.Usage{Output: 1_000_000},
	}, tbl)
	if c.Model != "m1" {
		t.Errorf("reported model = %q, want canonical m1", c.Model)
	}
	if c.Confidence != pricing.ConfidenceAlias {
		t.Errorf("confidence = %v, want alias", c.Confidence)
	}
	near(t, c.USD, 100)
}

func TestQuantisedModelReportsTheRateActuallyUsed(t *testing.T) {
	tbl := table()
	turn := model.Turn{Model: "m1-q4", Timestamp: at("2026-06-01"), Usage: model.Usage{Output: 1_000_000}}
	base := Of(turn, tbl)
	if base.Model != "m1" || base.Confidence != pricing.ConfidenceAlias {
		t.Fatalf("base fallback = %+v", base)
	}
	near(t, base.USD, 100)

	tbl.ByProvider["vendor\x00m1-q4"] = &pricing.Model{ID: "m1-q4", Rates: []pricing.Rate{{From: pricing.MustParseDate("2026-01-01"), Out: 9}}}
	turn.Provider = "vendor"
	exact := Of(turn, tbl)
	if exact.Model != "m1-q4" || exact.Confidence != pricing.ConfidenceProvider {
		t.Fatalf("provider rate = %+v", exact)
	}
	near(t, exact.USD, 9)
}

// Pricing a call with a rate that post-dates it is allowed but must be
// reported, so a backlog repriced by a later cut is visible.
func TestRateNewerThanCallIsFlagged(t *testing.T) {
	tbl := table()
	tbl.Models["m1"].Rates[0].From = pricing.MustParseDate("2026-08-01")

	older := Of(model.Turn{Model: "m1", Timestamp: at("2026-07-01"), Usage: model.Usage{Output: 1}}, tbl)
	if !older.RateNewerThanCall {
		t.Error("call predating every known rate was not flagged")
	}
	newer := Of(model.Turn{Model: "m1", Timestamp: at("2026-08-15"), Usage: model.Usage{Output: 1}}, tbl)
	if newer.RateNewerThanCall {
		t.Error("call after the rate took effect was wrongly flagged")
	}
}

// An agent can mix billing routes within one binary: OpenCode serves its own
// free models, bring-your-own-key API traffic, and a GitHub Copilot
// subscription. Calling all of it "metered API" misreports which dollars are
// real money.
func TestProviderDecidesBillingForMixedAgents(t *testing.T) {
	base := model.Turn{
		Model: "m1", Agent: model.AgentOpenCode,
		Timestamp: at("2026-06-01"), Usage: model.Usage{Output: 1_000_000},
	}
	for provider, want := range map[string]Billing{
		"openai":         BillingAPI,
		"anthropic":      BillingAPI,
		"moonshotai":     BillingAPI,
		"github-copilot": BillingSubscription,
		"ollama":         BillingLocal,
	} {
		base.Provider = provider
		if got := Of(base, table()).Billing; got != want {
			t.Errorf("provider %q billed as %v, want %v", provider, got, want)
		}
	}
}

func TestAggregateDoesNotInventLongContextTier(t *testing.T) {
	tbl := table()
	tbl.Models["m1"].Rates[0].TierThreshold = 200_000
	tbl.Models["m1"].Rates[0].TierIn = 20
	turn := model.Turn{Model: "m1", Timestamp: at("2026-06-01"), Usage: model.Usage{Input: 1_000_000}, Aggregate: true}
	near(t, Of(turn, tbl).USD, 10)
	turn.Aggregate = false
	near(t, Of(turn, tbl).USD, 20)
}

// The 1-hour cache write rate is stored as 2x the base input. Above the
// long-context threshold the input rate in force is the tier input, so a
// million 1-hour tokens on this card cost 12 dollars, not 6.
func TestLongContextScalesOneHourCacheWrite(t *testing.T) {
	tbl := table()
	rate := &tbl.Models["m1"].Rates[0]
	rate.In = 3
	rate.Out = 15
	rate.CacheWrite = 3.75
	rate.CacheWrite1h = 6
	rate.TierThreshold = 200_000
	rate.TierIn = 6
	rate.TierOut = 22.5
	rate.TierCacheWrite = 7.5
	turn := model.Turn{
		Model: "m1", Timestamp: at("2026-06-01"),
		Usage: model.Usage{CacheWrite: 1_000_000, CacheWrite1h: 1_000_000, ContextTokens: 250_000},
	}
	near(t, Of(turn, tbl).USD, 12)
	turn.Usage.ContextTokens = 100_000
	near(t, Of(turn, tbl).USD, 6)
	turn.Usage.ContextTokens = 250_000
	turn.Aggregate = true
	near(t, Of(turn, tbl).USD, 6)
}
