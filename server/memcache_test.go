package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"runtime/debug"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modpsapi                 = syscall.NewLazyDLL("psapi.dll")
	procGetProcessMemoryInfo = modpsapi.NewProc("GetProcessMemoryInfo")
)

// processMemoryCounters matches the Windows PROCESS_MEMORY_COUNTERS structure.
type processMemoryCounters struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

func getProcessMemoryInfo(process windows.Handle, pmc *processMemoryCounters, cb uint32) error {
	r1, _, e1 := procGetProcessMemoryInfo.Call(uintptr(process), uintptr(unsafe.Pointer(pmc)), uintptr(cb))
	if r1 == 0 {
		return e1
	}
	return nil
}

func currentProcessRSS() uint64 {
	var pmc processMemoryCounters
	pmc.CB = uint32(unsafe.Sizeof(pmc))
	if err := getProcessMemoryInfo(windows.CurrentProcess(), &pmc, pmc.CB); err != nil {
		return 0
	}
	return uint64(pmc.WorkingSetSize)
}

func mockSubsonicServerWithBigSong(t *testing.T, songSize int64) *subsonicClient {
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
		w.Write([]byte(fmt.Sprintf(`{
			"subsonic-response": {
				"status": "ok",
				"artist": {
					"id": "artist1",
					"name": "Test Artist",
					"album": [{"id": "album1", "name": "Test Album", "created": "2020-03-15T10:30:00Z"}]
				}
			}
		}`)))
	})
	mux.HandleFunc("/rest/getAlbum", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fmt.Sprintf(`{
			"subsonic-response": {
				"status": "ok",
				"album": {
					"id": "album1",
					"name": "Test Album",
					"song": [
						{"id": "song1", "path": "/music/song1.flac", "size": %d, "contentType": "audio/flac", "created": "2019-06-20T14:00:00Z"}
					]
				}
			}
		}`, songSize)))
	})
	mux.HandleFunc("/rest/stream", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id != "song1" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		start, end := int64(0), songSize-1
		if h := r.Header.Get("Range"); h != "" {
			_, _ = fmt.Sscanf(h, "bytes=%d-%d", &start, &end)
		}
		if start < 0 {
			start = 0
		}
		if start > songSize-1 {
			start = songSize - 1
		}
		if end >= songSize {
			end = songSize - 1
		}
		if end < start {
			end = start
		}
		length := end - start + 1
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, songSize))
		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
		w.WriteHeader(http.StatusPartialContent)
		buf := make([]byte, 64*1024)
		remaining := length
		for remaining > 0 {
			n := int64(len(buf))
			if n > remaining {
				n = remaining
			}
			w.Write(buf[:n])
			remaining -= n
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return newSubsonicClient(srv.URL, "user", "pass")
}

func cacheEntryCount(m *sync.Map) int {
	count := 0
	m.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

func TestMemCacheReleasesMemory(t *testing.T) {
	const songSize = 20 * 1024 * 1024
	client := mockSubsonicServerWithBigSong(t, songSize)
	fs := NewNavidromeFS(client, "", "")

	// Warm up and establish a clean baseline.
	runtime.GC()
	debug.FreeOSMemory()
	time.Sleep(100 * time.Millisecond)
	baseline := currentProcessRSS()
	t.Logf("Baseline RSS: %d MB", baseline/(1024*1024))

	f, err := fs.OpenFile("artist1/album1/song1.flac", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	copied, err := io.Copy(io.Discard, f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if copied != songSize {
		t.Fatalf("copied %d, want %d", copied, songSize)
	}

	// The 20 MB song should have populated the chunk cache.
	chunkCount := cacheEntryCount(&fs.chunkMemCache)
	t.Logf("Chunk cache entries after reading: %d", chunkCount)
	expectedChunks := int(songSize / fetchChunkSize)
	if chunkCount < expectedChunks/2 {
		t.Fatalf("expected at least %d chunk cache entries, got %d", expectedChunks/2, chunkCount)
	}

	// Wait for the 1-second TTL to expire, plus a buffer for timers.
	// The user requested waiting ~5 seconds after stopping playback.
	time.Sleep(5 * time.Second)

	// The timer callbacks call maybeFreeOSMemory, but they are throttled to
	// once per 5s. Force a final return-to-OS for the test measurement.
	debug.FreeOSMemory()
	time.Sleep(100 * time.Millisecond)

	// Verify the cache is empty.
	if n := cacheEntryCount(&fs.chunkMemCache); n != 0 {
		t.Fatalf("chunk cache still has %d entries after TTL", n)
	}
	if n := cacheEntryCount(&fs.coverMemCache); n != 0 {
		t.Fatalf("cover cache still has %d entries after TTL", n)
	}

	after := currentProcessRSS()
	t.Logf("After reading %d MB and waiting TTL: RSS %d MB", songSize/(1024*1024), after/(1024*1024))

	// We check the *delta* rather than an absolute number because the test binary
	// and runtime may already have a baseline RSS. The 20 MB cache should be gone.
	if after > baseline+5*1024*1024 {
		t.Fatalf("RSS grew by %d MB after cache expiry; expected cache to be released", (after-baseline)/(1024*1024))
	}
}
