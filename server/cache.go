package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// cacheLocks serializes read-modify-write cycles of a cache file, keyed by the
// song id. Without it, concurrent CacheChunk calls for the same song could
// record the same .dat offset and overwrite each other's index entry.
var cacheLocks sync.Map // filename -> *sync.RWMutex

func cacheLock(filename string) *sync.RWMutex {
	v, _ := cacheLocks.LoadOrStore(filename, &sync.RWMutex{})
	return v.(*sync.RWMutex)
}

type CacheEntry struct {
	Start     int   `json:"start"`
	End       int   `json:"end"`
	DatOffset int64 `json:"datOffset"`
	Length    int   `json:"length"`
}

type CacheIndex struct {
	FullSize int64        `json:"fullSize"`
	MIMEType string       `json:"mimeType"`
	Created  string       `json:"created"`
	Entries  []CacheEntry `json:"entries"`
}

func ReadSongMeta(filename, cacheDir string) (size int64, mime, created string, ok bool) {
	cacheDir = cacheDirPath(cacheDir)
	mu := cacheLock(filename)
	mu.RLock()
	defer mu.RUnlock()
	idx := readIndex(filename, cacheDir)
	if idx == nil || idx.FullSize == 0 {
		return 0, "", "", false
	}
	return idx.FullSize, idx.MIMEType, idx.Created, true
}

func SaveSongMeta(filename string, size int64, mime, created, cacheDir string) error {
	cacheDir = cacheDirPath(cacheDir)
	os.MkdirAll(cacheDir, 0755)
	mu := cacheLock(filename)
	mu.Lock()
	defer mu.Unlock()
	idx := readIndex(filename, cacheDir)
	if idx != nil {
		if idx.FullSize == size && idx.MIMEType == mime && idx.Created == created {
			return nil
		}
		idx.FullSize = size
		idx.MIMEType = mime
		idx.Created = created
		return writeIndex(filename, cacheDir, idx)
	}
	return writeIndex(filename, cacheDir, &CacheIndex{FullSize: size, MIMEType: mime, Created: created})
}

func deleteCachedFiles(filename, cacheDir string) {
	cacheDir = cacheDirPath(cacheDir)
	mu := cacheLock(filename)
	mu.Lock()
	defer mu.Unlock()
	os.Remove(idxPath(filename, cacheDir))
	os.Remove(datPath(filename, cacheDir))
}

func idxPath(filename, cacheDir string) string {
	return filepath.Join(cacheDir, filename+".idx")
}

func datPath(filename, cacheDir string) string {
	return filepath.Join(cacheDir, filename+".dat")
}

func readIndex(filename, cacheDir string) *CacheIndex {
	raw, err := os.ReadFile(idxPath(filename, cacheDir))
	if err != nil {
		return nil
	}
	var idx CacheIndex
	if err := json.Unmarshal(raw, &idx); err != nil {
		return nil
	}
	return &idx
}

func writeIndex(filename, cacheDir string, idx *CacheIndex) error {
	raw, err := json.Marshal(idx)
	if err != nil {
		return err
	}
	return os.WriteFile(idxPath(filename, cacheDir), raw, 0644)
}

func cacheDirPath(cacheDir string) string {
	if cacheDir == "" {
		return ".navicache"
	}
	return cacheDir
}

func CacheChunk(filename string, start int, data []byte, cacheDir string) error {
	cacheDir = cacheDirPath(cacheDir)
	os.MkdirAll(cacheDir, 0755)

	mu := cacheLock(filename)
	mu.Lock()
	defer mu.Unlock()

	idx := readIndex(filename, cacheDir)
	if idx == nil {
		idx = &CacheIndex{FullSize: int64(0), MIMEType: ""}
	}

	for _, e := range idx.Entries {
		if e.Start == start {
			return nil
		}
	}

	var datOffset int64
	if fi, err := os.Stat(datPath(filename, cacheDir)); err == nil {
		datOffset = fi.Size()
	}

	f, err := os.OpenFile(datPath(filename, cacheDir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	n, err := f.Write(data)
	if err != nil {
		return err
	}

	idx.Entries = append(idx.Entries, CacheEntry{
		Start:     start,
		End:       start + len(data) - 1,
		DatOffset: datOffset,
		Length:    n,
	})

	return writeIndex(filename, cacheDir, idx)
}

func ReadCachedRange(filename string, start, end int, cacheDir string) ([]byte, error) {
	cacheDir = cacheDirPath(cacheDir)

	mu := cacheLock(filename)
	mu.RLock()
	defer mu.RUnlock()

	idx := readIndex(filename, cacheDir)
	if idx == nil {
		return nil, nil
	}

	var best *CacheEntry
	for _, e := range idx.Entries {
		if start >= e.Start && end <= e.End {
			if best == nil || (e.End-e.Start) < (best.End-best.Start) {
				best = &e
			}
		}
	}
	if best == nil {
		return nil, nil
	}

	f, err := os.Open(datPath(filename, cacheDir))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	offsetInChunk := int64(start - best.Start)
	readOffset := best.DatOffset + offsetInChunk
	length := int64(end - start + 1)

	buf := make([]byte, length)
	if _, err := f.ReadAt(buf, readOffset); err != nil {
		return nil, err
	}

	return buf, nil
}

func ReadFull(filename, cacheDir string) ([]byte, error) {
	cacheDir = cacheDirPath(cacheDir)
	idx := readIndex(filename, cacheDir)
	if idx == nil || len(idx.Entries) == 0 {
		return nil, nil
	}

	maxEnd := 0
	for _, e := range idx.Entries {
		if e.End > maxEnd {
			maxEnd = e.End
		}
	}

	return ReadCachedRange(filename, 0, maxEnd, cacheDir)
}

func cachedIDs(cacheDir string) []string {
	cacheDir = cacheDirPath(cacheDir)
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if len(name) < 4 {
			continue
		}
		id := name[:len(name)-4]
		if name[len(name)-4:] == ".idx" && !seen[id] {
			seen[id] = true
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
