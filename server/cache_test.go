package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var testCacheDir = filepath.Join(os.Getenv("LOCALAPPDATA"), "navifsp", "testcache")

func cleanup() {
	os.RemoveAll(testCacheDir)
}

func TestCache(t *testing.T) {
	t.Run("store and retrieve single chunk", func(t *testing.T) {
		cleanup()
		data := []byte("HELLO FROM BYTE 0")
		if err := CacheChunk("s1", 0, data, testCacheDir); err != nil {
			t.Fatal(err)
		}
		result, err := ReadCachedRange("s1", 0, len(data)-1, testCacheDir)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(result, data) {
			t.Fatalf("expected %q, got %q", data, result)
		}
	})

	t.Run("appends multiple chunks at different offsets", func(t *testing.T) {
		cleanup()
		chunk1 := []byte("ALPHA")
		chunk2 := []byte("BETA")
		if err := CacheChunk("s2", 100, chunk1, testCacheDir); err != nil {
			t.Fatal(err)
		}
		if err := CacheChunk("s2", 500, chunk2, testCacheDir); err != nil {
			t.Fatal(err)
		}
		r1, err := ReadCachedRange("s2", 100, 104, testCacheDir)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(r1, chunk1) {
			t.Fatalf("expected %q, got %q", chunk1, r1)
		}
		r2, err := ReadCachedRange("s2", 500, 503, testCacheDir)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(r2, chunk2) {
			t.Fatalf("expected %q, got %q", chunk2, r2)
		}
		miss, err := ReadCachedRange("s2", 0, 4, testCacheDir)
		if err != nil {
			t.Fatal(err)
		}
		if miss != nil {
			t.Fatal("expected nil for uncached range")
		}
	})

	t.Run("returns nil for uncached file", func(t *testing.T) {
		cleanup()
		result, err := ReadCachedRange("nonexistent", 0, 10, testCacheDir)
		if err != nil {
			t.Fatal(err)
		}
		if result != nil {
			t.Fatal("expected nil for uncached file")
		}
	})

	t.Run("partial read within a cached chunk", func(t *testing.T) {
		cleanup()
		data := []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZ")
		if err := CacheChunk("s4", 200, data, testCacheDir); err != nil {
			t.Fatal(err)
		}
		r1, err := ReadCachedRange("s4", 205, 209, testCacheDir)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(r1, []byte("FGHIJ")) {
			t.Fatalf("expected %q, got %q", "FGHIJ", r1)
		}
	})

	t.Run("integrity with streamed song", func(t *testing.T) {
		client := navidromeClient(t)
		ctx := context.Background()
		cleanup()
		name := "test_song"

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
		t.Logf("Song: %q (%s)", parts[len(parts)-1], s.ID)

		body, err := client.streamSong(ctx, s.ID)
		if err != nil {
			t.Fatal(err)
		}
		defer body.Close()
		original, err := io.ReadAll(body)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Full song size: %d bytes", len(original))

		origHash := sha256.Sum256(original)

		partSize := (len(original) + 2) / 3
		for i := 0; i < 3; i++ {
			start := i * partSize
			end := start + partSize
			if end > len(original) {
				end = len(original)
			}
			if err := CacheChunk(name, start, original[start:end], testCacheDir); err != nil {
				t.Fatal(err)
			}
		}

		var reconstructed []byte
		for i := 0; i < 3; i++ {
			start := i * partSize
			end := start + partSize
			if end > len(original) {
				end = len(original)
			}
			chunk, err := ReadCachedRange(name, start, end-1, testCacheDir)
			if err != nil {
				t.Fatal(err)
			}
			reconstructed = append(reconstructed, chunk...)
		}

		reconHash := sha256.Sum256(reconstructed)

		if reconHash != origHash {
			t.Fatal("SHA-256 mismatch — cache corrupted the data")
		}

		t.Logf("SHA-256: %x", origHash)

		datFi, _ := os.Stat(testCacheDir + "/" + name + ".dat")
		idxFi, _ := os.Stat(testCacheDir + "/" + name + ".idx")
		t.Logf("  .dat size: %d bytes", datFi.Size())
		t.Logf("  .idx size: %d bytes (3 entries)", idxFi.Size())
		t.Logf("  Overhead: %.4f%%", float64(idxFi.Size())/float64(len(original))*100)
	})
}
