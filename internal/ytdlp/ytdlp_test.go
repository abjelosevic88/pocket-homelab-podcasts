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

func TestSourceURL(t *testing.T) {
	// a renamed handle must not break a followed channel: index by its id
	if got := SourceURL("https://www.youtube.com/@t3dotgg/videos", KindChannel, "UCbRP3c757lWg9M-U7TyEkXA"); got != "https://www.youtube.com/channel/UCbRP3c757lWg9M-U7TyEkXA/videos" {
		t.Fatalf("channel: %s", got)
	}
	if got := SourceURL("https://www.youtube.com/@x/videos", KindChannel, ""); got != "https://www.youtube.com/@x/videos" {
		t.Fatalf("no id: %s", got)
	}
	pl := "https://www.youtube.com/playlist?list=PLabc"
	if got := SourceURL(pl, KindPlaylist, "PLabc"); got != pl {
		t.Fatalf("playlist: %s", got)
	}
}

func TestVideoFromShare(t *testing.T) {
	for _, c := range []struct{ in, id string }{
		{"https://youtu.be/dQw4w9WgXcQ?si=AbCdEf123", "dQw4w9WgXcQ"},
		{"https://youtube.com/shorts/dQw4w9WgXcQ?si=x", "dQw4w9WgXcQ"},
		{"https://m.youtube.com/watch?v=dQw4w9WgXcQ&list=PL123&index=2", "dQw4w9WgXcQ"},
		{"https://youtu.be/dQw4w9WgXcQ?list=PLabc", "dQw4w9WgXcQ"},
		{"https://music.youtube.com/watch?v=dQw4w9WgXcQ&feature=share", "dQw4w9WgXcQ"},
		{"Watch this! https://youtu.be/dQw4w9WgXcQ?si=zz via YouTube", "dQw4w9WgXcQ"},
		{"  https://www.youtube.com/live/dQw4w9WgXcQ\n", "dQw4w9WgXcQ"},
	} {
		norm, id, err := VideoFromShare(c.in)
		if err != nil || id != c.id || norm != "https://www.youtube.com/watch?v="+c.id {
			t.Errorf("%q → %q %q %v", c.in, norm, id, err)
		}
	}
	for _, bad := range []string{"https://www.youtube.com/@theo", "https://www.youtube.com/playlist?list=PL123", "https://vimeo.com/1", "hello"} {
		if _, _, err := VideoFromShare(bad); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
}
