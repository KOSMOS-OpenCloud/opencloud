package search

import "testing"

func TestParseSkipSources(t *testing.T) {
	got := parseSkipSources(" Saskia, archikart,,")
	if len(got) != 2 || !got["saskia"] || !got["archikart"] {
		t.Fatalf("parseSkipSources = %v", got)
	}
	if len(parseSkipSources("")) != 0 {
		t.Fatal("empty value must give no sources")
	}
}

func TestSkipDocMetaFor(t *testing.T) {
	s := &Service{docMetaSkipSources: parseSkipSources("saskia")}
	cases := []struct {
		meta map[string]string
		want bool
	}{
		{map[string]string{"source": "saskia", "doc.type": "IfrAnordnung"}, true},
		{map[string]string{"source": "SASKIA"}, true},
		{map[string]string{"source": "taki"}, false},
		{map[string]string{"doc.type": "rechnung"}, false},
		{nil, false},
	}
	for _, tc := range cases {
		if got := s.skipDocMetaFor(tc.meta); got != tc.want {
			t.Errorf("skipDocMetaFor(%v) = %v, want %v", tc.meta, got, tc.want)
		}
	}
	if (&Service{}).skipDocMetaFor(map[string]string{"source": "saskia"}) {
		t.Error("without configured sources nothing is skipped")
	}
}
