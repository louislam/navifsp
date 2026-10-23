package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func namingMockServer(t *testing.T) (*subsonicClient, *int32) {
	t.Helper()
	var requests int32
	mux := http.NewServeMux()
	count := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&requests, 1)
			h(w, r)
		}
	}

	mux.HandleFunc("/rest/getArtists", count(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"subsonic-response":{"status":"ok","artists":{"index":[{"artist":[{"id":"artist1","name":"Test Artist"}]}]}}}`))
	}))
	mux.HandleFunc("/rest/getArtist", count(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"subsonic-response":{"status":"ok","artist":{"id":"artist1","name":"Test Artist","album":[{"id":"album1","name":"Test Album","artistId":"artist1","created":"2020-01-01T00:00:00Z"}]}}}`))
	}))
	mux.HandleFunc("/rest/getAlbum", count(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"subsonic-response":{"status":"ok","album":{"id":"album1","name":"Test Album","artistId":"artist1","created":"2020-01-01T00:00:00Z","song":[
			{"id":"s1","title":"First","track":1,"path":"/m/s1.flac","size":100,"contentType":"audio/flac","created":"2020-01-02T00:00:00Z"},
			{"id":"s2","title":"Second","track":2,"path":"/m/s2.mp3","size":200,"contentType":"audio/mpeg","created":"2020-01-03T00:00:00Z"}
		]}}}`))
	}))
	mux.HandleFunc("/rest/getCoverArt", count(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "4")
			return
		}
		w.Write([]byte{0xff, 0xd8, 0xff, 0xd9})
	}))
	mux.HandleFunc("/rest/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 100))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return newSubsonicClient(srv.URL, "user", "pass"), &requests
}

func TestSplitIDName(t *testing.T) {
	tests := []struct {
		segment string
		want    string
		ok      bool
	}{
		{"Artist [abc123]", "abc123", true},
		{"Album [x] [id1]", "id1", true},
		{"[id1]", "", false},
		{"Artist [abc", "", false},
		{"Artist abc]", "", false},
		{"Artist []", "", false},
		{"Artist [a/b]", "", false},
		{"Artist", "", false},
	}

	for _, tt := range tests {
		got, ok := splitIDName(tt.segment)
		if ok != tt.ok || got != tt.want {
			t.Errorf("splitIDName(%q) = (%q, %v), want (%q, %v)", tt.segment, got, ok, tt.want, tt.ok)
		}
	}
}

func TestSplitSongFileName(t *testing.T) {
	tests := []struct {
		segment string
		id      string
		ext     string
		ok      bool
	}{
		{"01 - Title [song1].flac", "song1", ".flac", true},
		{"Title [a.b].flac", "a.b", ".flac", true},
		{"Title [song1]", "song1", "", true},
		{"Title [song1].flac.tmp", "song1", ".flac.tmp", true},
		{"01 - Title.flac", "", "", false},
		{"cover.jpg", "", "", false},
	}

	for _, tt := range tests {
		id, ext, ok := splitSongFileName(tt.segment)
		if ok != tt.ok || id != tt.id || ext != tt.ext {
			t.Errorf("splitSongFileName(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.segment, id, ext, ok, tt.id, tt.ext, tt.ok)
		}
	}
}

func TestSanitizeComponent(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{`a/b:c*d?e"f<g>h|i`, "a_b_c_d_e_f_g_h_i"},
		{"CON", "_CON"},
		{"con.txt", "_con.txt"},
		{"trail. ", "trail"},
		{"...", ""},
		{"", ""},
		{"日本語", "日本語"},
		{"a\x01b", "a_b"},
	}

	for _, tt := range tests {
		if got := sanitizeComponent(tt.in); got != tt.want {
			t.Errorf("sanitizeComponent(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDisplayNameWithID(t *testing.T) {
	tests := []struct {
		display string
		id      string
		want    string
	}{
		{"Test Artist", "a1", "Test Artist [a1]"},
		{"CON", "a2", "_CON [a2]"},
		{"", "a3", "a3 [a3]"},
		{"Trailing. ", "a4", "Trailing [a4]"},
	}

	for _, tt := range tests {
		if got := displayNameWithID(tt.display, tt.id); got != tt.want {
			t.Errorf("displayNameWithID(%q, %q) = %q, want %q", tt.display, tt.id, got, tt.want)
		}
	}
}

func TestSongFileName(t *testing.T) {
	tests := []struct {
		name      string
		song      song
		multiDisc bool
		want      string
	}{
		{
			name: "single disc",
			song: song{ID: "s1", Title: "First Song", Track: 1, Path: "/m/s1.flac"},
			want: "01 - First Song [s1].flac",
		},
		{
			name:      "multi disc",
			song:      song{ID: "s2", Title: "Second", Track: 3, DiscNumber: 2, Path: "/m/s2.flac"},
			multiDisc: true,
			want:      "2-03 - Second [s2].flac",
		},
		{
			name: "no track number",
			song: song{ID: "s3", Title: "No Track", Path: "/m/s3.mp3"},
			want: "No Track [s3].mp3",
		},
		{
			name: "no title falls back to path",
			song: song{ID: "s4", Path: "/m/original.flac"},
			want: "original [s4].flac",
		},
	}

	for _, tt := range tests {
		if got := songFileName(tt.song, tt.multiDisc); got != tt.want {
			t.Errorf("%s: songFileName() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestSongFileNameSanitizesAndTruncates(t *testing.T) {
	s := song{ID: "s5", Title: `A/B:C*D?"E"<F>G|H`, Track: 1, Path: "/m/s5.flac"}
	name := songFileName(s, false)
	if strings.ContainsAny(name, `<>:"/\|?*`) {
		t.Fatalf("file name still contains illegal characters: %q", name)
	}
	if !strings.HasSuffix(name, " [s5].flac") {
		t.Fatalf("ID suffix missing from %q", name)
	}

	long := song{ID: "s6", Title: strings.Repeat("あ", 300), Track: 1, Path: "/m/s6.flac"}
	name = songFileName(long, false)
	if units := utf16Len(name); units > maxPathComponent {
		t.Fatalf("file name uses %d UTF-16 units, want <= %d: %q", units, maxPathComponent, name)
	}
	if !strings.HasSuffix(name, " [s6].flac") {
		t.Fatalf("ID suffix missing from truncated name %q", name)
	}
}

func TestDisplayNameTruncates(t *testing.T) {
	name := displayNameWithID(strings.Repeat("あ", 300), "artist1")
	if units := utf16Len(name); units > maxPathComponent {
		t.Fatalf("display name uses %d UTF-16 units, want <= %d", units, maxPathComponent)
	}
	if !strings.HasSuffix(name, " [artist1]") {
		t.Fatalf("ID suffix missing from truncated name %q", name)
	}
}

func TestListingThenOpeningSongsMakesNoExtraRequests(t *testing.T) {
	client, requests := namingMockServer(t)
	fs := NewNavidromeFS(client, "", "")

	root, err := fs.OpenFile("", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	rootEntries, err := root.Readdir(-1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rootEntries) != 1 || rootEntries[0].Name() != "Test Artist [artist1]" {
		t.Fatalf("unexpected root entries: %v", rootEntries)
	}

	artist, err := fs.OpenFile("Test Artist [artist1]", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	albumEntries, err := artist.Readdir(-1)
	if err != nil {
		t.Fatal(err)
	}
	if len(albumEntries) != 1 || albumEntries[0].Name() != "Test Album [album1]" {
		t.Fatalf("unexpected artist entries: %v", albumEntries)
	}

	albumPath := "Test Artist [artist1]/Test Album [album1]"
	album, err := fs.OpenFile(albumPath, os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	songEntries, err := album.Readdir(-1)
	if err != nil {
		t.Fatal(err)
	}
	names := make(map[string]bool)
	for _, e := range songEntries {
		names[e.Name()] = true
	}
	for _, want := range []string{"01 - First [s1].flac", "02 - Second [s2].mp3", CoverArtName} {
		if !names[want] {
			t.Fatalf("album listing missing %q: %v", want, names)
		}
	}

	before := atomic.LoadInt32(requests)
	for _, name := range []string{"01 - First [s1].flac", "02 - Second [s2].mp3"} {
		info, err := fs.Stat(albumPath + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() == 0 {
			t.Fatalf("%s has zero size", name)
		}
		f, err := fs.OpenFile(albumPath+"/"+name, os.O_RDONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	if after := atomic.LoadInt32(requests); after != before {
		t.Fatalf("song Stat/Open made %d extra requests after listing", after-before)
	}
}

func TestColdSongOpenCallsOnlyGetAlbum(t *testing.T) {
	client, requests := namingMockServer(t)
	fs := NewNavidromeFS(client, "", "")

	f, err := fs.OpenFile("Test Artist [artist1]/Test Album [album1]/01 - First [s1].flac", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	if n := atomic.LoadInt32(requests); n != 1 {
		t.Fatalf("cold song open made %d requests, want 1 (getAlbum)", n)
	}
}

func TestOldIDPathsAreRejected(t *testing.T) {
	client, requests := namingMockServer(t)
	fs := NewNavidromeFS(client, "", "")

	for _, path := range []string{
		"artist1",
		"artist1/album1",
		"artist1/album1/s1.flac",
	} {
		if _, err := fs.Stat(path); err == nil {
			t.Fatalf("old ID path %q must not resolve", path)
		}
		if _, err := fs.OpenFile(path, os.O_RDONLY, 0); err == nil {
			t.Fatalf("old ID path %q must not open", path)
		}
	}
	if n := atomic.LoadInt32(requests); n != 0 {
		t.Fatalf("old ID paths made %d API requests, want 0", n)
	}
}

func TestAlbumArtistMismatchIsRejected(t *testing.T) {
	client, _ := namingMockServer(t)
	fs := NewNavidromeFS(client, "", "")

	if _, err := fs.Stat("Other Artist [other]/Test Album [album1]"); err == nil {
		t.Fatal("album under a mismatched artist must not resolve")
	}
}

func TestGetAlbumCachesMetadata(t *testing.T) {
	client, requests := namingMockServer(t)
	album, err := client.getAlbum(context.Background(), "album1")
	if err != nil {
		t.Fatal(err)
	}
	if album.Name != "Test Album" || album.ArtistID != "artist1" || len(album.Songs) != 2 {
		t.Fatalf("unexpected album: %+v", album)
	}
	if _, err := client.getAlbum(context.Background(), "album1"); err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(requests); n != 1 {
		t.Fatalf("getAlbum hit the server %d times, want 1", n)
	}
}
