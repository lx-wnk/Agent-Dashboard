package pipeline

import "testing"

func TestLatestModel_ComparesVersionsNumerically(t *testing.T) {
	saved := allowedModelIDs
	t.Cleanup(func() { allowedModelIDs = saved })
	allowedModelIDs = map[string]bool{
		"claude-opus-4-8":   true,
		"claude-opus-4-10":  true,
		"claude-sonnet-5":   true,
		"claude-sonnet-5-1": true,
		"claude-haiku-4-5":  true,
	}

	cases := map[string]string{
		SeriesOpus:   "claude-opus-4-10",
		SeriesSonnet: "claude-sonnet-5-1",
		SeriesHaiku:  "claude-haiku-4-5",
	}
	for series, want := range cases {
		if got := LatestModel(series); got != want {
			t.Errorf("LatestModel(%q) = %q, want %q", series, got, want)
		}
	}
}

func TestLatestModel_PanicsWithoutAModelInTheSeries(t *testing.T) {
	saved := allowedModelIDs
	t.Cleanup(func() { allowedModelIDs = saved })
	allowedModelIDs = map[string]bool{"claude-opus-5": true}

	defer func() {
		if recover() == nil {
			t.Fatal("LatestModel(haiku) with no haiku model must panic, not return a default")
		}
	}()
	LatestModel(SeriesHaiku)
}
