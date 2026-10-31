package naming

import "testing"

func TestParseFilename(t *testing.T) {
	cases := []struct {
		in   string
		want Parsed
	}{
		{"The.Boys.S01E04.MULTi.HDR.2160p.WEB.DTS-HD.MA.5.1.H265-Wawacity.red.mkv", Parsed{Title: "The Boys", Season: 1, Episode: 4, HasEpisode: true, Ext: ".mkv"}},
		{"Dunkirk.2017.MULTi.1080p.BluRay.x264-LOST.mkv", Parsed{Title: "Dunkirk", Year: 2017, Ext: ".mkv"}},
		{"Arcane.S02.E05.VOSTFR.1080p.mp4", Parsed{Title: "Arcane", Season: 2, Episode: 5, HasEpisode: true, Ext: ".mp4"}},
		{"breaking_bad_3x07_720p.avi", Parsed{Title: "breaking bad", Season: 3, Episode: 7, HasEpisode: true, Ext: ".avi"}},
		{"Arcane.S02.Bonus.Making.Of.MULTi.mkv", Parsed{Title: "Arcane", Season: 2, Ext: ".mkv"}},
		{"Spirit.Untamed.2021.MULTi.1080p.WEB.mkv", Parsed{Title: "Spirit Untamed", Year: 2021, Ext: ".mkv"}},
		{"French.Kiss.1995.MULTi.1080p.mkv", Parsed{Title: "French Kiss", Year: 1995, Ext: ".mkv"}},
		{"1917.2019.MULTi.2160p.mkv", Parsed{Title: "1917", Year: 2019, Ext: ".mkv"}},
	}
	for _, c := range cases {
		if got := ParseFilename(c.in); got != c.want {
			t.Errorf("ParseFilename(%q) = %+v, attendu %+v", c.in, got, c.want)
		}
	}
}
