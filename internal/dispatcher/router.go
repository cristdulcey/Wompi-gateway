package dispatcher

import (
	"sort"
	"strings"

	"github.com/loducode/wompi-gateway/internal/domain"
)

// ParsePrefixes splits a comma-separated ReferencePrefix string into individual trimmed prefixes.
// An empty string or "*" returns nil (indicating a catch-all fallback rule).
func ParsePrefixes(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "*" {
		return nil
	}
	parts := strings.Split(trimmed, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		token := strings.TrimSpace(p)
		if token == "" || token == "*" {
			continue
		}
		if _, exists := seen[token]; !exists {
			seen[token] = struct{}{}
			out = append(out, token)
		}
	}
	return out
}

// NormalizePrefixString formats a single or comma-separated prefix string consistently (e.g. "order|, sub|, topup|").
func NormalizePrefixString(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "*" {
		return trimmed
	}
	tokens := ParsePrefixes(trimmed)
	if len(tokens) == 0 {
		return "*"
	}
	return strings.Join(tokens, ", ")
}

type matchedRuleCandidate struct {
	rule       *domain.RouteRule
	matchedLen int
}

// MatchRoute evaluates a transaction reference against a list of RouteRules and returns the best match.
// Rules are evaluated as follows:
//  1. Only Active rules are considered.
//  2. A rule's ReferencePrefix may contain one prefix ("sub-") or multiple comma-separated prefixes ("order|, sub|, topup|").
//  3. Non-empty, non-"*" prefixes that match strings.HasPrefix(reference, prefix) are sorted by:
//     - longest matched prefix first
//     - highest Priority first
//     - deterministic ID tie-breaker
//  4. If no specific prefix rule matches, fallback catch-all rules (ReferencePrefix == "" or "*")
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
		prefixes := ParsePrefixes(rule.ReferencePrefix)
		if len(prefixes) == 0 {
			fallbackMatches = append(fallbackMatches, rule)
			continue
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
