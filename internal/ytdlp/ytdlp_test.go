package ytdlp

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct{ in, kind, norm, id string }{
		{"https://www.youtube.com/@Fireship", KindChannel, "https://www.youtube.com/@Fireship/videos", ""},
		{"https://www.youtube.com/@Fireship/videos", KindChannel, "https://www.youtube.com/@Fireship/videos", ""},
		{"@t3dotgg", KindChannel, "https://www.youtube.com/@t3dotgg/videos", ""},
		{"https://www.youtube.com/channel/UCsBjURrPoezykLs9EqgamOA", KindChannel, "https://www.youtube.com/channel/UCsBjURrPoezykLs9EqgamOA/videos", "UCsBjURrPoezykLs9EqgamOA"},
		{"https://youtube.com/playlist?list=PLQWAYEAq7TDo&si=abc", KindPlaylist, "https://www.youtube.com/playlist?list=PLQWAYEAq7TDo", "PLQWAYEAq7TDo"},
		{"https://www.youtube.com/watch?v=aqz-KE-bpKQ&list=PLx", KindVideo, "https://www.youtube.com/watch?v=aqz-KE-bpKQ", "aqz-KE-bpKQ"},
		{"https://youtu.be/aqz-KE-bpKQ", KindVideo, "https://www.youtube.com/watch?v=aqz-KE-bpKQ", "aqz-KE-bpKQ"},
		{"https://www.youtube.com/shorts/aqz-KE-bpKQ", KindVideo, "https://www.youtube.com/watch?v=aqz-KE-bpKQ", "aqz-KE-bpKQ"},
	}
	for _, c := range cases {
		kind, norm, id, err := Classify(c.in)
		if err != nil || kind != c.kind || norm != c.norm || id != c.id {
			t.Errorf("%s → %s %s %s %v (want %s %s %s)", c.in, kind, norm, id, err, c.kind, c.norm, c.id)
		}
	}
	if _, _, _, err := Classify("https://vimeo.com/123"); err == nil {
		t.Error("vimeo accepted")
	}
}
