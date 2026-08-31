package api

import (
	"reflect"
	"testing"
)

// TestDiffRoutes exercises diffRoutes directly with synthetic route sets. It
// is what actually proves the comparison logic can fail: no wiring in the
// production router today produces a route present under bypass auth but
// missing once auth is on, so TestRouteGolden_AuthOnlyRoutes's reverse-direction
// assertion (diffRoutes(bypassRoutes, authOnRoutes) must be empty) has never
// gone red. This test is not that — it proves diffRoutes reports an
// asymmetric set correctly in both directions; it says nothing about whether
// the real router can ever produce one.
func TestDiffRoutes(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want []string
	}{
		{
			name: "identical sets yield no diff",
			a:    []string{"GET /a", "GET /b"},
			b:    []string{"GET /a", "GET /b"},
			want: nil,
		},
		{
			name: "both empty yields no diff",
			a:    nil,
			b:    nil,
			want: nil,
		},
		{
			name: "route only in a is reported",
			a:    []string{"GET /a", "GET /extra"},
			b:    []string{"GET /a"},
			want: []string{"GET /extra"},
		},
		{
			name: "route only in b is not reported (a-b direction)",
			a:    []string{"GET /a"},
			b:    []string{"GET /a", "GET /extra"},
			want: nil,
		},
		{
			name: "duplicate line in a that is also in b is excluded for every occurrence",
			a:    []string{"GET /a", "GET /a", "GET /extra"},
			b:    []string{"GET /a"},
			want: []string{"GET /extra"},
		},
		{
			name: "duplicate line in a absent from b is reported once per occurrence (no dedup)",
			a:    []string{"GET /a", "GET /extra", "GET /extra"},
			b:    []string{"GET /a"},
			want: []string{"GET /extra", "GET /extra"},
		},
		{
			name: "output preserves a's (sorted) order",
			a:    []string{"GET /a", "GET /m", "GET /z"},
			b:    []string{"GET /m"},
			want: []string{"GET /a", "GET /z"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := diffRoutes(tc.a, tc.b)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("diffRoutes(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestDiffRoutes_ReverseDirection proves the exact shape guarded by
// TestRouteGolden_AuthOnlyRoutes's "vanished" check: given a route present
// under bypass auth but absent once auth is on, diffRoutes(bypass, authOn)
// reports it. This demonstrates the reverse-direction assertion is
// red-capable against the comparison helper — it does not demonstrate that
// the production router can ever produce such a set.
func TestDiffRoutes_ReverseDirection(t *testing.T) {
	bypassRoutes := []string{"GET /always", "GET /bypass-only"}
	authOnRoutes := []string{"GET /always"}

	vanished := diffRoutes(bypassRoutes, authOnRoutes)
	want := []string{"GET /bypass-only"}
	if !reflect.DeepEqual(vanished, want) {
		t.Fatalf("diffRoutes(bypass, authOn) = %v, want %v", vanished, want)
	}
}
