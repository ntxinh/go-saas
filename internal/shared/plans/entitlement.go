// Package plans is the static plan-entitlement table. It is shared
// mechanism (a pure lookup), not a feature: feature slices like orgs
// call Can to enforce seat limits without importing a billing feature.
// -1 means unlimited; missing plans/features deny.
package plans

var limits = map[string]map[string]int{
	"free":       {"members": 3, "projects": 1},
	"pro":        {"members": 50, "projects": 100},
	"enterprise": {"*": -1},
}

// Can reports whether plan allows n units of feature (e.g.
// Can("free", "members", 4) → false). Returns false for unknown
// plans and features — fail closed.
func Can(plan, feature string, n int) bool {
	feats, ok := limits[plan]
	if !ok {
		return false
	}
	limit, ok := feats[feature]
	if !ok {
		limit, ok = feats["*"]
		if !ok {
			return false
		}
	}
	return limit < 0 || n <= limit
}
