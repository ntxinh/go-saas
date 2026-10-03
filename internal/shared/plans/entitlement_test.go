package plans_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/exodia/go-saas/internal/shared/plans"
)

// Can is the static plan-entitlement table: free caps members at 3,
// pro at 50, enterprise is unlimited, unknown plans/features deny.
func TestCan(t *testing.T) {
	cases := []struct {
		name    string
		plan    string
		feature string
		n       int
		want    bool
	}{
		{"free members at cap", "free", "members", 3, true},
		{"free members over cap", "free", "members", 4, false},
		{"free project at cap", "free", "projects", 1, true},
		{"free project over cap", "free", "projects", 2, false},
		{"pro members at cap", "pro", "members", 50, true},
		{"pro members over cap", "pro", "members", 51, false},
		{"pro projects", "pro", "projects", 100, true},
		{"pro projects over", "pro", "projects", 101, false},
		{"enterprise members unlimited", "enterprise", "members", 10000, true},
		{"enterprise any feature", "enterprise", "anything", 99, true},
		{"unknown plan denied", "startup", "members", 1, false},
		{"unknown feature denied", "free", "sso", 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, plans.Can(tc.plan, tc.feature, tc.n))
		})
	}
}
