package settings

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const zentraleOnly = `{"version":1,"pages":[{"id":"zentrale","title":"Zentrale","tiles":[` +
	`{"widget":"agents","col":1,"row":1,"colSpan":3,"rowSpan":3},` +
	`{"widget":"obsidian__recent","col":4,"row":1,"colSpan":6,"rowSpan":2}]}]}`

func TestWorkspaceLayout_RegisteredAsLiveString(t *testing.T) {
	d, ok := Lookup("workspace.layout")
	require.True(t, ok, "workspace.layout is not in the registry")
	require.Equal(t, TypeString, d.Type)
	require.Equal(t, ApplyLive, d.Apply)
	require.Equal(t, "", d.Default)
}

func TestWorkspaceLayout_Validation(t *testing.T) {
	d, _ := Lookup("workspace.layout")

	// Empty means "the built-in layout".
	require.NoError(t, d.Validate(""))
	// An unknown widget id is kept: a deactivated module must not cost the layout.
	require.NoError(t, d.Validate(zentraleOnly))

	// The settings API accepts a PATCH from anything on loopback, so the rules
	// the browser applies must hold here too.
	bad := map[string]string{
		"not json":         `{`,
		"wrong version":    strings.Replace(zentraleOnly, `"version":1`, `"version":2`, 1),
		"unknown field":    strings.Replace(zentraleOnly, `"version":1`, `"version":1,"extra":true`, 1),
		"overlap":          strings.Replace(zentraleOnly, `"col":4,"row":1`, `"col":3,"row":2`, 1),
		"past column 12":   strings.Replace(zentraleOnly, `"col":4,"row":1,"colSpan":6`, `"col":8,"row":1,"colSpan":6`, 1),
		"row below one":    strings.Replace(zentraleOnly, `"col":4,"row":1`, `"col":4,"row":0`, 1),
		"span below one":   strings.Replace(zentraleOnly, `"colSpan":6,"rowSpan":2`, `"colSpan":6,"rowSpan":0`, 1),
		"bad widget id":    strings.Replace(zentraleOnly, `obsidian__recent`, `Bad Widget`, 1),
		"no zentrale":      strings.Replace(zentraleOnly, `"id":"zentrale"`, `"id":"morning"`, 1),
		"duplicate page":   strings.Replace(zentraleOnly, `]}]}`, `]},{"id":"zentrale","title":"Again","tiles":[]}]}`, 1),
		"empty title":      strings.Replace(zentraleOnly, `"title":"Zentrale"`, `"title":"  "`, 1),
		"uppercase pageid": strings.Replace(zentraleOnly, `]}]}`, `]},{"id":"Morning","title":"M","tiles":[]}]}`, 1),
	}
	for name, raw := range bad {
		require.Error(t, d.Validate(raw), name)
	}
}
