package dispatcher

import (
	"sort"
	"strings"

	"github.com/loducode/wompi-gateway/internal/domain"
)

// MatchRoute evaluates a transaction reference against a list of RouteRules and returns the best match.
// Rules are evaluated as follows:
//  1. Only Active rules are considered.
//  2. Non-empty, non-"*" prefixes that match strings.HasPrefix(reference, prefix) are sorted by:
//     - longest ReferencePrefix first
//     - highest Priority first
//     - deterministic ID tie-breaker
//  3. If no specific prefix rule matches, fallback catch-all rules (ReferencePrefix == "" or "*")
//     are evaluated ordered by highest Priority first.
func MatchRoute(reference string, rules []*domain.RouteRule) *domain.RouteRule {
	if len(rules) == 0 {
		return nil
	}

	var specificMatches []*domain.RouteRule
	var fallbackMatches []*domain.RouteRule

	for _, rule := range rules {
		if rule == nil || !rule.Active {
			continue
		}
		prefix := strings.TrimSpace(rule.ReferencePrefix)
		if prefix == "" || prefix == "*" {
			fallbackMatches = append(fallbackMatches, rule)
			continue
		}
		if strings.HasPrefix(reference, prefix) {
			specificMatches = append(specificMatches, rule)
		}
	}

	if len(specificMatches) > 0 {
		sort.SliceStable(specificMatches, func(i, j int) bool {
			lenI := len(strings.TrimSpace(specificMatches[i].ReferencePrefix))
			lenJ := len(strings.TrimSpace(specificMatches[j].ReferencePrefix))
			if lenI != lenJ {
				return lenI > lenJ
			}
			if specificMatches[i].Priority != specificMatches[j].Priority {
				return specificMatches[i].Priority > specificMatches[j].Priority
			}
			return specificMatches[i].ID < specificMatches[j].ID
		})
		return specificMatches[0]
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
