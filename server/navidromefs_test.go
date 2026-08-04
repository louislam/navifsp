package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joho/godotenv"
)

func navidromeClient(t *testing.T) *subsonicClient {
	t.Helper()
	godotenv.Load(".env")
	baseURL := os.Getenv("NAVIFSP_BASE_URL")
	username := os.Getenv("NAVIFSP_USERNAME")
	password := os.Getenv("NAVIFSP_PASSWORD")
	if baseURL == "" || username == "" || password == "" {
		t.Skip("Set NAVIFSP_BASE_URL, NAVIFSP_USERNAME, NAVIFSP_PASSWORD")
	}
	return newSubsonicClient(baseURL, username, password)
}

func TestGetArtists(t *testing.T) {
	client := navidromeClient(t)
	ctx := context.Background()

	artists, err := client.getArtists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(artists) == 0 {
		t.Fatal("expected at least 1 artist")
	}
	t.Logf("Got %d artists", len(artists))
	var names []string
	for i, a := range artists {
		if i >= 3 {
			break
		}
		names = append(names, a.Name)
	}
	t.Logf("First 3: %v", names)
	if artists[0].ID == "" {
		t.Fatal("artist missing id")
	}
	if artists[0].Name == "" {
		t.Fatal("artist missing name")
	}
}

func TestGetAlbums(t *testing.T) {
	client := navidromeClient(t)
	ctx := context.Background()

	artists, err := client.getArtists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(artists) == 0 {
		t.Fatal("no artists found")
	}
	a := artists[0]
	t.Logf("Using artist: %q (%s)", a.Name, a.ID)

	albums, err := client.getAlbums(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) == 0 {
		t.Fatal("expected at least 1 album")
	}
	t.Logf("Got %d albums", len(albums))
	var names []string
	for i, al := range albums {
		if i >= 3 {
			break
		}
		names = append(names, al.Name)
	}
	t.Logf("First 3: %v", names)
	if albums[0].ID == "" {
		t.Fatal("album missing id")
	}
	if albums[0].Name == "" {
		t.Fatal("album missing name")
	}
}

func TestGetSongs(t *testing.T) {
	client := navidromeClient(t)
	ctx := context.Background()

	artists, err := client.getArtists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(artists) == 0 {
		t.Fatal("no artists found")
	}
	albums, err := client.getAlbums(ctx, artists[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) == 0 {
		t.Fatal("no albums found")
	}
	al := albums[0]
	t.Logf("Using album: %q (%s)", al.Name, al.ID)

	songs, err := client.getSongs(ctx, al.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(songs) == 0 {
		t.Fatal("expected at least 1 song")
	}
	t.Logf("Got %d songs", len(songs))
	var fnames []string
	for i, s := range songs {
		if i >= 3 {
			break
		}
		parts := strings.Split(s.Path, "/")
		fnames = append(fnames, parts[len(parts)-1])
	}
	t.Logf("First 3: %v", fnames)
	if songs[0].ID == "" {
		t.Fatal("song missing id")
	}
	if songs[0].Path == "" {
		t.Fatal("song missing path")
	}
}

func TestStreamSong(t *testing.T) {
	client := navidromeClient(t)
	ctx := context.Background()

	artists, err := client.getArtists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(artists) == 0 {
		t.Fatal("no artists found")
	}
	albums, err := client.getAlbums(ctx, artists[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) == 0 {
		t.Fatal("no albums found")
	}
	songs, err := client.getSongs(ctx, albums[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(songs) == 0 {
		t.Fatal("no songs found")
	}
	s := songs[0]
	parts := strings.Split(s.Path, "/")
	t.Logf("Using song: %q (%s)", parts[len(parts)-1], s.ID)

	body, err := client.streamSong(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()

	chunk := make([]byte, 1024)
	n, err := io.ReadFull(body, chunk)
	if err != nil && err != io.ErrUnexpectedEOF {
		t.Fatal(err)
	}
	t.Logf("Streamed first %d bytes", n)

	if n < 4 {
		t.Fatal("expected at least 4 bytes of stream data")
	}
	if string(chunk[:4]) != "fLaC" {
		t.Logf("Warning: not a FLAC file, header: %x", chunk[:4])
	}
}

func TestDumpRawSongJSON(t *testing.T) {
	client := navidromeClient(t)
	ctx := context.Background()

	artists, err := client.getArtists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(artists) == 0 {
		t.Fatal("no artists found")
	}
	albums, err := client.getAlbums(ctx, artists[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) == 0 {
		t.Fatal("no albums found")
	}
	songs, err := client.getSongs(ctx, albums[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(songs) == 0 {
		t.Fatal("no songs found")
	}

	// Fetch raw JSON for a single song via getSong endpoint
	s := songs[0]
	body, err := client.doRequest(ctx, client.buildURL("getSong", map[string]string{"id": s.ID}))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Raw JSON for song %s:\n%s", s.ID, string(body))
}

func TestDumpRawAlbumJSON(t *testing.T) {
	client := navidromeClient(t)
	ctx := context.Background()

	artists, err := client.getArtists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(artists) == 0 {
		t.Fatal("no artists found")
	}
	albums, err := client.getAlbums(ctx, artists[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) == 0 {
		t.Fatal("no albums found")
	}

	// Fetch raw JSON for a single album via getAlbum endpoint
	al := albums[0]
	body, err := client.doRequest(ctx, client.buildURL("getAlbum", map[string]string{"id": al.ID}))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Raw JSON for album %s:\n%s", al.ID, string(body))
}

func TestFSStatRoot(t *testing.T) {
	client := navidromeClient(t)
	fs := NewNavidromeFS(client, "", "")

	info, err := fs.Stat("")
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatal("expected root to be a directory")
	}
}

func TestFSReadDirRoot(t *testing.T) {
	client := navidromeClient(t)
	fs := NewNavidromeFS(client, "", "")

	f, err := fs.OpenFile("", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	entries, err := f.Readdir(-1)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least 1 entry in root")
	}
	t.Logf("Root has %d entries", len(entries))

	dirCount := 0
	for _, e := range entries {
		if e.IsDir() {
			dirCount++
		}
	}
	if dirCount == 0 {
		t.Fatal("expected at least 1 directory entry (artist)")
	}
}

func TestFSStatAndReadDirArtist(t *testing.T) {
	client := navidromeClient(t)
	fs := NewNavidromeFS(client, "", "")

	artists, err := client.getArtists(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(artists) == 0 {
		t.Fatal("no artists found")
	}
	artistPath := artists[0].ID
	t.Logf("Artist path: %s (%s)", artistPath, artists[0].Name)

	info, err := fs.Stat(artistPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatal("expected artist to be a directory")
	}

	f, err := fs.OpenFile(artistPath, os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	entries, err := f.Readdir(-1)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least 1 album")
	}
	t.Logf("Artist has %d albums", len(entries))
}

func TestFSStatAndReadDirAlbum(t *testing.T) {
	client := navidromeClient(t)
	fs := NewNavidromeFS(client, "", "")

	artists, err := client.getArtists(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(artists) == 0 {
		t.Fatal("no artists found")
	}
	albums, err := client.getAlbums(context.Background(), artists[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) == 0 {
		t.Fatal("no albums found")
	}
	albumPath := artists[0].ID + "/" + albums[0].ID
	t.Logf("Album path: %s (%s)", albumPath, albums[0].Name)

	info, err := fs.Stat(albumPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatal("expected album to be a directory")
	}

	f, err := fs.OpenFile(albumPath, os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	entries, err := f.Readdir(-1)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least 1 song")
	}
	t.Logf("Album has %d entries", len(entries))
	hasCover := false
	for _, e := range entries {
		if e.Name() == CoverArtName {
			hasCover = true
			break
		}
	}
	if !hasCover {
		t.Fatal("expected " + CoverArtName + " in album listing")
	}
}

func TestFSStatAndOpenCoverJpg(t *testing.T) {
	client := navidromeClient(t)
	fs := NewNavidromeFS(client, "", "")

	artists, err := client.getArtists(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(artists) == 0 {
		t.Fatal("no artists found")
	}
	albums, err := client.getAlbums(context.Background(), artists[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) == 0 {
		t.Fatal("no albums found")
	}
	coverPath := artists[0].ID + "/" + albums[0].ID + "/" + CoverArtName

	info, err := fs.Stat(coverPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.IsDir() {
		t.Fatal("expected " + CoverArtName + " to be a file")
	}
	t.Logf("Cover path: %s, stat size: %d", info.Name(), info.Size())

	f, err := fs.OpenFile(coverPath, os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Cover image: %d bytes", len(data))
	if len(data) < 100 {
		t.Fatal("cover image too small")
	}

	if rs, ok := f.(io.ReadSeeker); ok {
		rs.Seek(0, io.SeekStart)
		buf := make([]byte, 4)
		io.ReadFull(rs, buf)
		t.Logf("JPEG header: %x", buf)
	}
}

func TestFSOpenSong(t *testing.T) {
	client := navidromeClient(t)
	fs := NewNavidromeFS(client, "", "")

	artists, err := client.getArtists(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(artists) == 0 {
		t.Fatal("no artists found")
	}
	albums, err := client.getAlbums(context.Background(), artists[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) == 0 {
		t.Fatal("no albums found")
	}
	songs, err := client.getSongs(context.Background(), albums[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(songs) == 0 {
		t.Fatal("no songs found")
	}
	s := songs[0]
	ext := ""
	if dot := strings.LastIndex(s.Path, "."); dot >= 0 {
		ext = s.Path[dot:]
	}
	songPath := artists[0].ID + "/" + albums[0].ID + "/" + s.ID + ext
	t.Logf("Song path: %s", songPath)

	f, err := fs.OpenFile(songPath, os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	buf := make([]byte, 1024)
	n, err := f.ReadAt(buf, 0)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	t.Logf("ReadAt(0) returned %d bytes", n)
	if n < 4 {
		t.Fatal("expected at least 4 bytes")
	}
	if string(buf[:4]) == "fLaC" {
		t.Log("Detected FLAC header")
	}

	if rs, ok := f.(io.ReadSeeker); ok {
		rs.Seek(0, io.SeekStart)
		readBuf := make([]byte, 1024)
		n2, err := io.ReadFull(rs, readBuf)
		if err != nil && err != io.ErrUnexpectedEOF {
			t.Fatal(err)
		}
		t.Logf("Seek+Read returned %d bytes", n2)
	}
}

func TestFSOpenSongUsesRangeRequestsWithoutCacheDir(t *testing.T) {
	client := navidromeClient(t)
	fs := NewNavidromeFS(client, "", "")

	artists, err := client.getArtists(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(artists) == 0 {
		t.Fatal("no artists found")
	}
	albums, err := client.getAlbums(context.Background(), artists[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) == 0 {
		t.Fatal("no albums found")
	}
	songs, err := client.getSongs(context.Background(), albums[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(songs) == 0 {
		t.Fatal("no songs found")
	}
	s := songs[0]
	ext := ""
	if dot := strings.LastIndex(s.Path, "."); dot >= 0 {
		ext = s.Path[dot:]
	}
	songPath := artists[0].ID + "/" + albums[0].ID + "/" + s.ID + ext

	verbose = true
	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	defer func() {
		log.SetOutput(os.Stderr)
		verbose = false
	}()

	f, err := fs.OpenFile(songPath, os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	buf := make([]byte, 1024)
	n, err := f.ReadAt(buf, 0)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if n < 4 {
		t.Fatal("expected at least 4 bytes")
	}
	if string(buf[:4]) != "fLaC" {
		t.Fatalf("expected FLAC header, got %x", buf[:4])
	}

	logs := logBuf.String()
	if strings.Contains(logs, "Navidrome /rest/stream (full stream)") {
		t.Fatal("expected range requests (readSeeker) without cacheDir, but got full stream download")
	}
	if !strings.Contains(logs, "FetchAt") && !strings.Contains(logs, "Fetch ") {
		t.Fatal("expected FetchAt or Fetch log from readSeeker")
	}
	t.Log("Verified: song opened via range requests, no full stream download")
}

func TestFSCoverCache(t *testing.T) {
	client := navidromeClient(t)
	cacheDir := t.TempDir()
	fs := NewNavidromeFS(client, "", cacheDir)

	artists, err := client.getArtists(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(artists) == 0 {
		t.Fatal("no artists found")
	}
	albums, err := client.getAlbums(context.Background(), artists[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) == 0 {
		t.Fatal("no albums found")
	}
	albumID := albums[0].ID
	coverPath := artists[0].ID + "/" + albumID + "/" + CoverArtName

	f, err := fs.OpenFile(coverPath, os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	data1, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(data1) < 100 {
		t.Fatalf("cover art too small: %d bytes", len(data1))
	}
	t.Logf("First read: %d bytes", len(data1))

	coverFile := fs.coverCachePath(albumID)
	cached, err := os.ReadFile(coverFile)
	if err != nil {
		t.Fatalf("cover cache file not written: %v", err)
	}
	if len(cached) != len(data1) {
		t.Fatalf("cached size %d != read size %d", len(cached), len(data1))
	}
	t.Logf("Cache file written: %s (%d bytes)", coverFile, len(cached))

	f2, err := fs.OpenFile(coverPath, os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	data2, err := io.ReadAll(f2)
	f2.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(data2) != len(data1) {
		t.Fatalf("second read size %d != first read size %d", len(data2), len(data1))
	}
	t.Logf("Second read: %d bytes (from cache)", len(data2))

	if !bytes.Equal(data1, data2) {
		t.Fatal("cached data differs from original")
	}
	t.Log("Verified: cover art cache write/read consistency")
}

func mockSubsonicServer(t *testing.T) *subsonicClient {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/getArtists", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"subsonic-response": {
				"status": "ok",
				"artists": {
					"index": [{
						"name": "A",
						"artist": [{"id": "artist1", "name": "Test Artist"}]
					}]
				}
			}
		}`))
	})
	mux.HandleFunc("/rest/getArtist", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"subsonic-response": {
				"status": "ok",
				"artist": {
					"id": "artist1",
					"name": "Test Artist",
					"album": [{"id": "album1", "name": "Test Album", "created": "2020-03-15T10:30:00Z"}]
				}
			}
		}`))
	})
	mux.HandleFunc("/rest/getAlbum", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"subsonic-response": {
				"status": "ok",
				"album": {
					"id": "album1",
					"name": "Test Album",
					"song": [
						{"id": "song1", "path": "/music/song1.flac", "size": 1024, "contentType": "audio/flac", "created": "2019-06-20T14:00:00Z"},
						{"id": "song2", "path": "/music/song2.mp3", "size": 512, "contentType": "audio/mpeg", "created": "2021-01-10T08:00:00Z"}
					]
				}
			}
		}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return newSubsonicClient(srv.URL, "user", "pass")
}

func TestOpenFileSongUsesCreatedTimestamp(t *testing.T) {
	client := mockSubsonicServer(t)
	fs := NewNavidromeFS(client, "", "")

	f, err := fs.OpenFile("artist1/album1/song1.flac", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	expected := time.Date(2019, 6, 20, 14, 0, 0, 0, time.UTC)
	if !info.ModTime().Equal(expected) {
		t.Fatalf("song modTime = %v, want %v", info.ModTime(), expected)
	}
}

func TestStatSongUsesCreatedTimestamp(t *testing.T) {
	client := mockSubsonicServer(t)
	fs := NewNavidromeFS(client, "", "")

	info, err := fs.Stat("artist1/album1/song2.mp3")
	if err != nil {
		t.Fatal(err)
	}
	expected := time.Date(2021, 1, 10, 8, 0, 0, 0, time.UTC)
	if !info.ModTime().Equal(expected) {
		t.Fatalf("song stat modTime = %v, want %v", info.ModTime(), expected)
	}
}

func TestAlbumDirListingSongModTimes(t *testing.T) {
	client := mockSubsonicServer(t)
	fs := NewNavidromeFS(client, "", "")

	f, err := fs.OpenFile("artist1/album1", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	entries, err := f.Readdir(-1)
	if err != nil {
		t.Fatal(err)
	}

	expected := map[string]time.Time{
		"song1.flac": time.Date(2019, 6, 20, 14, 0, 0, 0, time.UTC),
		"song2.mp3":  time.Date(2021, 1, 10, 8, 0, 0, 0, time.UTC),
	}
	for _, e := range entries {
		if exp, ok := expected[e.Name()]; ok {
			if !e.ModTime().Equal(exp) {
				t.Fatalf("%s modTime = %v, want %v", e.Name(), e.ModTime(), exp)
			}
		}
	}
}

func TestGetAlbumsDedupByArtistId(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/getArtist", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "artist1" {
			w.Write([]byte(`{
				"subsonic-response": {
					"status": "ok",
					"artist": {
						"id": "artist1",
						"name": "Artist One",
						"album": [
							{"id": "albumA", "name": "Album A", "artistId": "artist1", "created": "2020-01-01T00:00:00Z"},
							{"id": "albumB", "name": "Album B", "artistId": "artist2", "created": "2020-02-01T00:00:00Z"}
						]
					}
				}
			}`))
		} else {
			w.Write([]byte(`{"subsonic-response": {"status": "ok", "artist": {"id": "artist2", "name": "Artist Two", "album": []}}}`))
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := newSubsonicClient(srv.URL, "user", "pass")
	albums, err := client.getAlbums(context.Background(), "artist1")
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 1 {
		t.Fatalf("expected 1 album after dedup, got %d: %v", len(albums), albums)
	}
	if albums[0].ID != "albumA" {
		t.Fatalf("expected albumA, got %s", albums[0].ID)
	}
}

func TestGetAlbumsIncludesMatchingArtistId(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/getArtist", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"subsonic-response": {
				"status": "ok",
				"artist": {
					"id": "artist1",
					"name": "Artist One",
					"album": [
						{"id": "albumA", "name": "Album A", "artistId": "artist1", "created": "2020-01-01T00:00:00Z"},
						{"id": "albumB", "name": "Album B", "created": "2020-02-01T00:00:00Z"}
					]
				}
			}
		}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := newSubsonicClient(srv.URL, "user", "pass")
	albums, err := client.getAlbums(context.Background(), "artist1")
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 2 {
		t.Fatalf("expected 2 albums (no artistId mismatch), got %d", len(albums))
	}
}

func TestStreamSongRangeRejectsErrorStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/stream", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := newSubsonicClient(srv.URL, "user", "pass")
	body, err := client.streamSongRange(context.Background(), "song1", 0, 1023)
	if err == nil {
		if body != nil {
			body.Close()
		}
		t.Fatal("expected error for 500 stream response, got nil")
	}
}

func TestGetCoverArtRejectsErrorStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/getCoverArt", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := newSubsonicClient(srv.URL, "user", "pass")
	body, err := client.getCoverArt(context.Background(), "album1")
	if err == nil {
		if body != nil {
			body.Close()
		}
		t.Fatal("expected error for 500 cover response, got nil")
	}
}

func TestCoverArtNotCachedOnServerError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/getCoverArt", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := newSubsonicClient(srv.URL, "user", "pass")
	cacheDir := t.TempDir()
	fs := NewNavidromeFS(client, "", cacheDir)

	_, err := fs.OpenFile("artist1/album1/cover.jpg", os.O_RDONLY, 0)
	if err == nil {
		t.Fatal("expected error opening cover when server returns 500")
	}
	if _, statErr := os.Stat(fs.coverCachePath("album1")); statErr == nil {
		t.Fatal("error body must not be written to the cover cache")
	}
}

func TestSongChunkNotCachedOnServerError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/getAlbum", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"subsonic-response": {
				"status": "ok",
				"album": {
					"id": "album1",
					"name": "Test Album",
					"song": [
						{"id": "song1", "path": "/music/song1.flac", "size": 102400, "contentType": "audio/flac", "created": "2019-06-20T14:00:00Z"}
					]
				}
			}
		}`))
	})
	mux.HandleFunc("/rest/stream", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := newSubsonicClient(srv.URL, "user", "pass")
	cacheDir := t.TempDir()
	fs := NewNavidromeFS(client, cacheDir, "")

	f, err := fs.OpenFile("artist1/album1/song1.flac", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	buf := make([]byte, 1024)
	if _, err := f.ReadAt(buf, 0); err == nil {
		t.Fatal("expected error reading song chunk when server returns 500")
	}

	if n := cacheEntryCount(&fs.chunkMemCache); n != 0 {
		t.Fatalf("error body cached in memory (%d entries)", n)
	}
	if _, statErr := os.Stat(filepath.Join(cacheDir, "song1.dat")); statErr == nil {
		t.Fatal("error body must not be written to the disk chunk cache")
	}
}

func TestParseTimeEmptyReturnsStableValue(t *testing.T) {
	a := parseTime("")
	time.Sleep(time.Millisecond)
	b := parseTime("")
	if !a.Equal(b) {
		t.Fatalf("parseTime(\"\") must return a stable value, got %v then %v", a, b)
	}
	if want := time.Unix(0, 0); !a.Equal(want) {
		t.Fatalf("parseTime(\"\") = %v, want %v", a, want)
	}

	c := parseTime("not-a-date")
	time.Sleep(time.Millisecond)
	d := parseTime("not-a-date")
	if !c.Equal(d) {
		t.Fatalf("parseTime(invalid) must return a stable value, got %v then %v", c, d)
	}
	if !c.Equal(time.Unix(0, 0)) {
		t.Fatalf("parseTime(invalid) = %v, want 1970-01-01", c)
	}
}

func TestGetSongIsCached(t *testing.T) {
	var songRequests int32
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/getSong", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&songRequests, 1)
		w.Write([]byte(`{"subsonic-response":{"status":"ok","song":{"id":"song1","path":"/music/song1.flac","size":1024,"contentType":"audio/flac","created":"2020-01-01T00:00:00Z"}}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := newSubsonicClient(srv.URL, "user", "pass")

	for i := 0; i < 2; i++ {
		s, err := client.getSong(context.Background(), "song1")
		if err != nil {
			t.Fatal(err)
		}
		if s.ID != "song1" {
			t.Fatalf("unexpected song: %+v", s)
		}
	}
	if n := atomic.LoadInt32(&songRequests); n != 1 {
		t.Fatalf("expected getSong to be cached, got %d HTTP requests", n)
	}
}

func TestAlbumListingCoverSizeUsesHeadNotFullDownload(t *testing.T) {
	const headSize = 54321
	var coverGets, coverHeads int32
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/getArtist", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"subsonic-response":{"status":"ok","artist":{"id":"artist1","name":"Artist One","album":[{"id":"album1","name":"Album A","artistId":"artist1","created":"2020-01-01T00:00:00Z"}]}}}`))
	})
	mux.HandleFunc("/rest/getAlbum", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"subsonic-response":{"status":"ok","album":{"id":"album1","name":"Album A","song":[{"id":"song1","path":"/music/song1.flac","size":1024,"contentType":"audio/flac","created":"2019-06-20T14:00:00Z"}]}}}`))
	})
	mux.HandleFunc("/rest/getCoverArt", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			atomic.AddInt32(&coverHeads, 1)
			w.Header().Set("Content-Length", strconv.Itoa(headSize))
			w.WriteHeader(http.StatusOK)
			return
		}
		atomic.AddInt32(&coverGets, 1)
		w.Write(bytes.Repeat([]byte{0xff}, 300))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := newSubsonicClient(srv.URL, "user", "pass")
	fs := NewNavidromeFS(client, "", "")

	f, err := fs.OpenFile("artist1/album1", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	entries, err := f.Readdir(-1)
	if err != nil {
		t.Fatal(err)
	}
	var coverSize int64 = -1
	for _, e := range entries {
		if e.Name() == CoverArtName {
			coverSize = e.Size()
			break
		}
	}
	if coverSize != headSize {
		t.Fatalf("expected cover size %d from HEAD, got %d", headSize, coverSize)
	}
	if n := atomic.LoadInt32(&coverGets); n != 0 {
		t.Fatalf("album listing must not download the full cover, got %d GETs", n)
	}
	if n := atomic.LoadInt32(&coverHeads); n != 1 {
		t.Fatalf("expected 1 HEAD for cover size, got %d", n)
	}
}

func TestCoverStatSizeFallsBackWhenHeadFails(t *testing.T) {
	fakeJPEG := bytes.Repeat([]byte{0xff}, 512)
	var coverGets int32
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/getArtist", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"subsonic-response":{"status":"ok","artist":{"id":"artist1","name":"Artist One","album":[{"id":"album1","name":"Album A","artistId":"artist1","created":"2020-01-01T00:00:00Z"}]}}}`))
	})
	mux.HandleFunc("/rest/getCoverArt", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			http.Error(w, "no head support", http.StatusInternalServerError)
			return
		}
		atomic.AddInt32(&coverGets, 1)
		w.Write(fakeJPEG)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := newSubsonicClient(srv.URL, "user", "pass")
	fs := NewNavidromeFS(client, "", "")

	info, err := fs.Stat("artist1/album1/cover.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != int64(len(fakeJPEG)) {
		t.Fatalf("expected cover size %d from fallback GET, got %d", len(fakeJPEG), info.Size())
	}
	if n := atomic.LoadInt32(&coverGets); n != 1 {
		t.Fatalf("expected exactly 1 fallback GET, got %d", n)
	}
}
