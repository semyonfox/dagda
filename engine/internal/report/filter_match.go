package report

import (
	"slices"
	"strings"

	"github.com/semyonfox/dagda/engine/internal/cost"
	"github.com/semyonfox/dagda/engine/internal/model"
	"github.com/semyonfox/dagda/engine/internal/pricing"
)

// FilterMatch records how one --project or --model query was resolved.
type FilterMatch struct {
	Filter string `json:"filter"`
	Query  string `json:"query"`
	// How is exact, label, contains, typo or none. Only exact and label are
	// spellings the user could have meant literally.
	How     string   `json:"how"`
	Matches []string `json:"matches,omitempty"`
}

// Loose reports whether the query was widened or failed, which is when the
// user should be told what it picked.
func (m FilterMatch) Loose() bool { return m.How != "exact" && m.How != "label" }

// looseHits is the fallback after the strict rules: a case-insensitive
// substring of the display name, then a name a typo or two away. A query
// keeps the first rule that hits anything.
func looseHits(query string, keys []string, display, typoName func(string) string) (string, []string) {
	q := strings.ToLower(query)
	var hits []string
	for _, key := range keys {
		if strings.Contains(strings.ToLower(display(key)), q) {
			hits = append(hits, key)
		}
	}
	if len(hits) > 0 {
		return "contains", hits
	}
	// roughly one edit per four characters, so `abc` cannot match half the list
	limit := max(1, len([]rune(q))/4)
	best := limit + 1
	for _, key := range keys {
		d := editDistance(q, strings.ToLower(typoName(key)))
		switch {
		case d < best:
			best, hits = d, []string{key}
		case d == best:
			hits = append(hits, key)
		}
	}
	if best > limit {
		return "none", nil
	}
	return "typo", hits
}

// resolveProjects turns each --project query into a selection, trying the
// strictest rule first: a recorded path or folder name, then the label the
// report prints, then the loose rules. So `orbit` never widens to `orbitmaps`
// when an orbit project exists.
func (c *projectCatalog) resolveProjects(f Filter) (Filter, []FilterMatch) {
	if len(f.Projects) == 0 {
		return f, nil
	}
	keys := make([]string, 0, len(c.byTurn))
	for _, ref := range c.byTurn {
		keys = append(keys, ref.key)
	}
	keys = uniqueSorted(keys)

	f.projectsResolved = true
	f.exactProjects = nil
	f.projectKeys = make(map[string]bool)
	matches := make([]FilterMatch, 0, len(f.Projects))
	for _, query := range f.Projects {
		m := FilterMatch{Filter: "project", Query: query}
		if hits := c.exactProjectHits(query); len(hits) > 0 {
			f.exactProjects = append(f.exactProjects, query)
			m.How, m.Matches = "exact", c.labelsOf(hits)
			matches = append(matches, m)
			continue
		}
		var hits []string
		for _, key := range keys {
			if strings.EqualFold(c.label(key), query) {
				hits = append(hits, key)
			}
		}
		m.How = "label"
		if len(hits) == 0 {
			m.How, hits = looseHits(query, keys, c.label, func(key string) string { return projectBaseLabel(key, c.home) })
		}
		for _, key := range hits {
			f.projectKeys[key] = true
		}
		m.Matches = c.labelsOf(hits)
		matches = append(matches, m)
	}
	return f, matches
}

// resolveModels does the same for --model. A recorded id or the id it
// resolves to is exact, so `--model claude-opus-5` still catches
// `anthropic/claude-opus-5`, and only then do the loose rules apply.
func resolveModels(f Filter, turns []model.Turn, tbl *pricing.Table) (Filter, []FilterMatch) {
	if len(f.Models) == 0 {
		return f, nil
	}
	// display name -> every spelling that shows under it
	names := make(map[string][]string)
	for _, t := range turns {
		c := cost.Of(t, tbl)
		shown := displayModel(t, c)
		names[shown] = append(names[shown], t.Model, c.Model)
	}
	shown := make([]string, 0, len(names))
	for name := range names {
		shown = append(shown, name)
	}
	slices.Sort(shown)

	f.modelsResolved = true
	f.modelNames = make(map[string]bool)
	matches := make([]FilterMatch, 0, len(f.Models))
	for _, query := range f.Models {
		m := FilterMatch{Filter: "model", Query: query}
		var hits []string
		for _, name := range shown {
			if slices.ContainsFunc(names[name], func(s string) bool { return s != "" && strings.EqualFold(s, query) }) {
				hits = append(hits, name)
			}
		}
		if len(hits) > 0 {
			m.How = "exact"
			f.modelNames[strings.ToLower(query)] = true
		} else {
			m.How, hits = looseHits(query, shown, func(s string) string { return s }, func(s string) string { return s })
			for _, name := range hits {
				for _, spelling := range names[name] {
					if spelling != "" {
						f.modelNames[strings.ToLower(spelling)] = true
					}
				}
			}
		}
		m.Matches = hits
		matches = append(matches, m)
	}
	return f, matches
}

// matchesModel applies a resolved model selection. An unresolved filter
// falls back to exact ids alone.
func (f Filter) matchesModel(raw, resolved string) bool {
	if !f.modelsResolved {
		return containsFold(f.Models, raw) || containsFold(f.Models, resolved)
	}
	return f.modelNames[strings.ToLower(raw)] || f.modelNames[strings.ToLower(resolved)]
}

// matchesProject applies a resolved project selection. An unresolved filter
// falls back to the strict rules alone.
func (f Filter) matchesProject(raw, key string) bool {
	if !f.projectsResolved {
		return matchProject(f.Projects, raw, key)
	}
	return f.projectKeys[key] || matchProject(f.exactProjects, raw, key)
}

func (c *projectCatalog) exactProjectHits(query string) []string {
	var hits []string
	for _, ref := range c.byTurn {
		if matchProject([]string{query}, ref.raw, ref.key) {
			hits = append(hits, ref.key)
		}
	}
	return uniqueSorted(hits)
}

func (c *projectCatalog) labelsOf(keys []string) []string {
	labels := make([]string, 0, len(keys))
	for _, key := range keys {
		labels = append(labels, c.label(key))
	}
	slices.Sort(labels)
	return slices.Compact(labels)
}

func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	cur := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		cur[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(br)]
}
