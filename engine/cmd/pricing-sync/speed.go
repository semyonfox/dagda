package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/semyonfox/dagda/engine/internal/pricing"
)

// models.dev has no priority or ultrafast prices, so they come from LiteLLM's
// list, which copies OpenAI's pricing page. The premium differs per model
// (1.75x to 2.5x across the gpt line), so one multiplier would be wrong.
const liteLLMURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

var liteLLMTierKey = regexp.MustCompile(`^input_cost_per_token_above_(\d+)k_tokens_(priority|ultrafast)$`)

// attachSpeedRates adds OpenAI's priority and ultrafast cards to the OpenAI
// first-party rates in ds. Only bare LiteLLM keys whose provider is openai
// are used, because azure and regional routes carry their own markups. It
// returns how many models gained a card.
func attachSpeedRates(ds *dataset, raw []byte) (int, error) {
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return 0, fmt.Errorf("litellm returned invalid JSON: %w", err)
	}
	cards := map[string][2]*pricing.SpeedRate{}
	for key, rawEntry := range entries {
		if strings.Contains(key, "/") {
			continue
		}
		var entry map[string]any
		if json.Unmarshal(rawEntry, &entry) != nil || entry["litellm_provider"] != "openai" {
			continue
		}
		id := pricing.Normalize(key)
		cards[id] = [2]*pricing.SpeedRate{speedCard(entry, "priority"), speedCard(entry, "ultrafast")}
	}
	attached := 0
	for id, m := range ds.Models {
		card, ok := cards[id]
		if !ok || len(m.Rates) != 1 || m.Rates[0].Source != "openai" {
			continue
		}
		if setSpeed(&m.Rates[0], card) {
			attached++
		}
		if vendor, ok := ds.ByProvider[providerLookupKey("openai", id)]; ok && len(vendor.Rates) == 1 {
			setSpeed(&vendor.Rates[0], card)
		}
	}
	return attached, nil
}

func setSpeed(r *pricing.Rate, card [2]*pricing.SpeedRate) bool {
	r.Priority = checkedSpeed(*r, card[0])
	r.Ultrafast = checkedSpeed(*r, card[1])
	return r.Priority != nil || r.Ultrafast != nil
}

// checkedSpeed keeps a card only when it is a premium over the standard one.
// A faster tier priced below standard is a units or mapping bug upstream. Tier
// fields only apply at the same long-context threshold as the standard card.
func checkedSpeed(r pricing.Rate, s *pricing.SpeedRate) *pricing.SpeedRate {
	if s == nil || s.In < r.In || s.Out < r.Out || s.In <= 0 || s.Out <= 0 {
		return nil
	}
	card := *s
	if r.TierThreshold <= 0 {
		card.TierIn, card.TierOut, card.TierCacheRead, card.TierCacheWrite = 0, 0, 0, 0
	}
	return &card
}

// speedCard reads one tier's card from a LiteLLM entry, in USD per 1M tokens.
func speedCard(entry map[string]any, tier string) *pricing.SpeedRate {
	field := func(name string) float64 {
		v, ok := entry[name].(float64)
		if !ok || v <= 0 {
			return 0
		}
		perM := math.Round(v*1e12) / 1e6
		if f, ok := sane(&perM); ok {
			return f
		}
		return 0
	}
	s := pricing.SpeedRate{
		In:         field("input_cost_per_token_" + tier),
		Out:        field("output_cost_per_token_" + tier),
		CacheRead:  field("cache_read_input_token_cost_" + tier),
		CacheWrite: field("cache_creation_input_token_cost_" + tier),
	}
	if s.In == 0 || s.Out == 0 {
		return nil
	}
	for name := range entry {
		match := liteLLMTierKey.FindStringSubmatch(name)
		if match == nil || match[2] != tier {
			continue
		}
		if _, err := strconv.Atoi(match[1]); err != nil {
			continue
		}
		suffix := "_above_" + match[1] + "k_tokens_" + tier
		s.TierIn = field("input_cost_per_token" + suffix)
		s.TierOut = field("output_cost_per_token" + suffix)
		s.TierCacheRead = field("cache_read_input_token_cost" + suffix)
		s.TierCacheWrite = field("cache_creation_input_token_cost" + suffix)
		break
	}
	return &s
}

// carrySpeedRates keeps the cards already published when LiteLLM can't be
// reached, so an outage doesn't read as every card being withdrawn.
func carrySpeedRates(prev, next *dataset) {
	if prev == nil {
		return
	}
	carry := func(prevModels, nextModels map[string]*pricing.Model) {
		for key, nm := range nextModels {
			pm, ok := prevModels[key]
			if !ok || len(pm.Rates) == 0 || len(nm.Rates) != 1 {
				continue
			}
			latest := pm.Rates[len(pm.Rates)-1]
			nm.Rates[0].Priority, nm.Rates[0].Ultrafast = latest.Priority, latest.Ultrafast
		}
	}
	carry(prev.Models, next.Models)
	carry(prev.ByProvider, next.ByProvider)
}

func sameSpeed(a, b *pricing.SpeedRate) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func warnf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
