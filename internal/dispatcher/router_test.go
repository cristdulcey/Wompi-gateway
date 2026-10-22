package dispatcher

import (
	"testing"

	"github.com/loducode/wompi-gateway/internal/domain"
)

func TestMatchRoute_OptimaxVsMrWaitersAndOverlappingPrefixes(t *testing.T) {
	rules := []*domain.RouteRule{
		{
			ID:              "rule-fallback",
			CompanyID:       "comp-default",
			Name:            "Default Catch-All",
			ReferencePrefix: "*",
			TargetURL:       "https://default.example.com/webhooks",
			Priority:        1,
			Active:          true,
		},
		{
			ID:              "rule-optimax",
			CompanyID:       "comp-optimax",
			Name:            "Optimax Subscriptions",
			ReferencePrefix: "sub-",
			TargetURL:       "https://api.optimax.example.com/webhooks/wompi",
			Priority:        10,
			Active:          true,
		},
		{
			ID:              "rule-optimax-vip",
			CompanyID:       "comp-optimax-vip",
			Name:            "Optimax VIP Subscriptions",
			ReferencePrefix: "sub-vip-",
			TargetURL:       "https://vip.optimax.example.com/webhooks/wompi",
			Priority:        5,
			Active:          true,
		},
		{
			ID:              "rule-mrwaiters",
			CompanyID:       "comp-mrwaiters",
			Name:            "Mr. Waiters Subscriptions",
			ReferencePrefix: "mw-sub-",
			TargetURL:       "https://api.mrwaiters.example.com/webhooks/wompi",
			Priority:        10,
			Active:          true,
		},
		{
			ID:              "rule-inactive",
			CompanyID:       "comp-inactive",
			Name:            "Inactive Rule",
			ReferencePrefix: "mw-sub-special-",
			TargetURL:       "https://inactive.example.com/webhooks",
			Priority:        100,
			Active:          false,
		},
	}

	tests := []struct {
		name          string
		reference     string
		expectedRoute string
	}{
		{
			name:          "Matches Optimax sub- prefix",
			reference:     "sub-88421-mensual",
			expectedRoute: "rule-optimax",
		},
		{
			name:          "Matches Mr. Waiters mw-sub- prefix without colliding with sub-",
			reference:     "mw-sub-99412-anual",
			expectedRoute: "rule-mrwaiters",
		},
		{
			name:          "Longest prefix wins over shorter prefix regardless of lower priority",
			reference:     "sub-vip-00192",
			expectedRoute: "rule-optimax-vip",
		},
		{
			name:          "Ignores inactive rule and falls back to active shorter prefix",
			reference:     "mw-sub-special-777",
			expectedRoute: "rule-mrwaiters",
		},
		{
			name:          "Uses catch-all fallback when no prefix matches",
			reference:     "pos-order-12345",
			expectedRoute: "rule-fallback",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			matched := MatchRoute(tc.reference, rules)
			if matched == nil {
				t.Fatalf("expected route %s, got nil", tc.expectedRoute)
			}
			if matched.ID != tc.expectedRoute {
				t.Errorf("expected route %s, got %s", tc.expectedRoute, matched.ID)
			}
		})
	}
}

func TestMatchRoute_NoMatchWithoutFallback(t *testing.T) {
	rules := []*domain.RouteRule{
		{
			ID:              "rule-optimax",
			ReferencePrefix: "sub-",
			Active:          true,
		},
	}

	if matched := MatchRoute("other-123", rules); matched != nil {
		t.Fatalf("expected nil match, got %+v", matched)
	}
}
