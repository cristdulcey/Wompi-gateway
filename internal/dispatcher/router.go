package dispatcher

import (
	"sort"
	"strings"

	"github.com/loducode/wompi-gateway/internal/domain"
)

// ParsePrefixConfig splits a ReferencePrefix string into specific non-wildcard prefixes
// and reports whether a wildcard fallback ("*" or empty) was included.
func ParsePrefixConfig(raw string) (prefixes []string, hasFallback bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "*" {
		return nil, true
	}
	parts := strings.Split(trimmed, ",")
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		token := strings.TrimSpace(p)
		if token == "" {
			continue
		}
		if token == "*" {
			hasFallback = true
			continue
		}
		if _, exists := seen[token]; !exists {
			seen[token] = struct{}{}
			prefixes = append(prefixes, token)
		}
	}
	if len(prefixes) == 0 && !hasFallback {
		return nil, true
	}
	return prefixes, hasFallback
}

// ParsePrefixes splits a comma-separated ReferencePrefix string into individual trimmed prefixes.
// An empty string or "*" returns nil (indicating a catch-all fallback rule).
func ParsePrefixes(raw string) []string {
	prefixes, _ := ParsePrefixConfig(raw)
	return prefixes
}

// NormalizePrefixString formats a single or comma-separated prefix string consistently (e.g. "order|, sub|, topup|" or "st-, *").
func NormalizePrefixString(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "*" {
		return "*"
	}
	prefixes, hasFallback := ParsePrefixConfig(trimmed)
	if len(prefixes) == 0 {
		return "*"
	}
	if hasFallback {
		return strings.Join(append(prefixes, "*"), ", ")
	}
	return strings.Join(prefixes, ", ")
}

type matchedRuleCandidate struct {
	rule       *domain.RouteRule
	matchedLen int
}

// MatchRoute evaluates a transaction reference against a list of RouteRules and returns the best match.
// Rules are evaluated as follows:
//  1. Only Active rules are considered.
//  2. A rule's ReferencePrefix may contain one prefix ("sub-"), multiple comma-separated prefixes ("order|, sub|, topup|"),
//     or a hybrid of prefixes and wildcard fallback ("st-, *").
//  3. Non-empty, non-"*" prefixes that match strings.HasPrefix(reference, prefix) are sorted by:
//     - longest matched prefix first
//     - highest Priority first
//     - deterministic ID tie-breaker
//  4. If no specific prefix rule matches, fallback catch-all rules (ReferencePrefix contains "*" or "")
//     are evaluated ordered by highest Priority first.
func MatchRoute(reference string, rules []*domain.RouteRule) *domain.RouteRule {
	if len(rules) == 0 {
		return nil
	}

	var specificMatches []matchedRuleCandidate
	var fallbackMatches []*domain.RouteRule

	for _, rule := range rules {
		if rule == nil || !rule.Active {
			continue
		}
		prefixes, hasFallback := ParsePrefixConfig(rule.ReferencePrefix)
		if hasFallback {
			fallbackMatches = append(fallbackMatches, rule)
		}

		bestLen := -1
		for _, prefix := range prefixes {
			if strings.HasPrefix(reference, prefix) && len(prefix) > bestLen {
				bestLen = len(prefix)
			}
		}
		if bestLen >= 0 {
			specificMatches = append(specificMatches, matchedRuleCandidate{
				rule:       rule,
				matchedLen: bestLen,
			})
		}
	}

	if len(specificMatches) > 0 {
		sort.SliceStable(specificMatches, func(i, j int) bool {
			if specificMatches[i].matchedLen != specificMatches[j].matchedLen {
				return specificMatches[i].matchedLen > specificMatches[j].matchedLen
			}
			if specificMatches[i].rule.Priority != specificMatches[j].rule.Priority {
				return specificMatches[i].rule.Priority > specificMatches[j].rule.Priority
			}
			return specificMatches[i].rule.ID < specificMatches[j].rule.ID
		})
		return specificMatches[0].rule
	}

	if len(fallbackMatches) > 0 {
		sort.SliceStable(fallbackMatches, func(i, j int) bool {
			if fallbackMatches[i].Priority != fallbackMatches[j].Priority {
				return fallbackMatches[i].Priority > fallbackMatches[j].Priority
			}
			return fallbackMatches[i].ID < fallbackMatches[j].ID
		})
		return fallbackMatches[0]
	}

	return nil
}
