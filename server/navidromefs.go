package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/winfsp/go-winfsp/gofs"
)

var (
	lastFreeMemMu   sync.Mutex
	lastFreeMemTime time.Time
)

// maybeFreeOSMemory asks the Go runtime to return freed heap memory to the OS,
// throttled so a burst of cache expirations does not trigger repeated GCs.
func maybeFreeOSMemory() {
	lastFreeMemMu.Lock()
	defer lastFreeMemMu.Unlock()
	if time.Since(lastFreeMemTime) < time.Second {
		return
	}
	lastFreeMemTime = time.Now()
	debug.FreeOSMemory()
}

var verbose bool

func debugLog(format string, args ...any) {
	if verbose {
		log.Printf(format, args...)
	}
}

// NavidromeFS implements gofs.FileSystem backed by the Navidrome Subsonic API.
type NavidromeFS struct {
	client        *subsonicClient
	cacheDir      string
	coverCacheDir string
	startTime     time.Time
	coverMemCache sync.Map
	chunkMemCache sync.Map
}

func NewNavidromeFS(client *subsonicClient, musicCacheDir string, coverCacheDir string) *NavidromeFS {
	return &NavidromeFS{
		client:        client,
		cacheDir:      musicCacheDir,
		coverCacheDir: coverCacheDir,
		startTime:     time.Now(),
	}
}

func parsePathParts(p string) []string {
	p = strings.TrimLeft(p, "/\\")
	if p == "" {
		return nil
	}
	p = strings.ReplaceAll(p, "/", "\\")
	return strings.Split(p, "\\")
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Unix(0, 0)
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05", s)
	}
	if err != nil {
		return time.Unix(0, 0)
	}
	return t
}

func (fs *NavidromeFS) coverCachePath(albumID string) string {
	return filepath.Join(fs.coverCacheDir, albumID)
}

func (fs *NavidromeFS) readCoverCache(albumID string) ([]byte, bool) {
	if fs.coverCacheDir == "" {
		return nil, false
	}
	data, err := os.ReadFile(fs.coverCachePath(albumID))
	if err != nil {
		return nil, false
	}
	return data, true
}

func (fs *NavidromeFS) writeCoverCache(albumID string, data []byte) {
	if fs.coverCacheDir == "" {
		return
	}
	os.MkdirAll(fs.coverCacheDir, 0755)
	os.WriteFile(fs.coverCachePath(albumID), data, 0644)
}

func (fs *NavidromeFS) readCoverMemCache(albumID string) ([]byte, bool) {
	val, ok := fs.coverMemCache.Load(albumID)
	if !ok {
		return nil, false
	}
	entry := val.(*memCacheEntry)
	if time.Now().After(entry.expiresAt) {
		fs.coverMemCache.Delete(albumID)
		return nil, false
	}
	return entry.data, true
}

func (fs *NavidromeFS) writeCoverMemCache(albumID string, data []byte) {
	entry := &memCacheEntry{
		data:      data,
		expiresAt: time.Now().Add(memCacheTTL),
	}
	entry.timer = time.AfterFunc(memCacheTTL, func() {
		fs.coverMemCache.CompareAndDelete(albumID, entry)
		maybeFreeOSMemory()
	})
	fs.coverMemCache.Store(albumID, entry)
}

// coverSize reports the byte size of an album's cover art using the cheapest
// available source. The full image is only downloaded when a HEAD request
// cannot provide a size (e.g. servers that do not respond to HEAD), in which
// case the result is cached.
func (fs *NavidromeFS) coverSize(ctx context.Context, albumID string) int64 {
	if data, ok := fs.readCoverMemCache(albumID); ok {
		return int64(len(data))
	}
	if fs.coverCacheDir != "" {
		if data, ok := fs.readCoverCache(albumID); ok {
			fs.writeCoverMemCache(albumID, data)
			return int64(len(data))
		}
	}
	size, err := fs.client.getCoverArtSize(ctx, albumID)
	if err == nil && size >= 0 {
		return size
	}
	rc, err := fs.client.getCoverArt(ctx, albumID)
	if err != nil {
		return 0
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return 0
	}
	fs.writeCoverMemCache(albumID, data)
	if fs.coverCacheDir != "" {
		fs.writeCoverCache(albumID, data)
	}
	return int64(len(data))
}

func (fs *NavidromeFS) Stat(name string) (os.FileInfo, error) {
	debugLog("Stat(%q)", name)
	parts := parsePathParts(name)

	if len(parts) == 0 {
		return &navFileInfo{
			name:    "",
			isDir:   true,
			modTime: fs.startTime}, nil
	}

	if len(parts) > 3 {
		return nil, os.ErrNotExist
	}

	ctx := context.Background()
	artistID, ok := splitIDName(parts[0])
	if !ok {
		return nil, os.ErrNotExist
	}

	if len(parts) == 1 {
		artists, err := fs.client.getArtists(ctx)
		if err != nil {
			return nil, err
		}
		for _, a := range artists {
			if a.ID == artistID {
				return &navFileInfo{name: displayNameWithID(a.Name, a.ID), isDir: true, modTime: fs.startTime}, nil
			}
		}
		return nil, os.ErrNotExist
	}

	albumID, ok := splitIDName(parts[1])
	if !ok {
		return nil, os.ErrNotExist
	}

	if len(parts) == 2 {
		album, err := fs.client.getAlbum(ctx, albumID)
		if err != nil {
			return nil, err
		}
		if !albumBelongsToArtist(album, artistID) {
			return nil, os.ErrNotExist
		}
		return &navFileInfo{name: displayNameWithID(album.Name, album.ID), isDir: true, modTime: parseTime(album.Created)}, nil
	}

	if parts[2] == CoverArtName {
		album, err := fs.client.getAlbum(ctx, albumID)
		if err != nil {
			return nil, err
		}
		if !albumBelongsToArtist(album, artistID) {
			return nil, os.ErrNotExist
		}
		size := fs.coverSize(ctx, albumID)
		return &navFileInfo{name: CoverArtName, size: size, modTime: fs.startTime, isDir: false}, nil
	}

	songID, _, ok := splitSongFileName(parts[2])
	if !ok {
		return nil, os.ErrNotExist
	}
	album, err := fs.client.getAlbum(ctx, albumID)
	if err != nil {
		return nil, err
	}
	if !albumBelongsToArtist(album, artistID) {
		return nil, os.ErrNotExist
	}
	multiDisc := isMultiDisc(album.Songs)
	for _, s := range album.Songs {
		if s.ID == songID {
			return &navFileInfo{name: songFileName(s, multiDisc), size: s.Size, modTime: parseTime(s.Created), isDir: false}, nil
		}
	}
	return nil, os.ErrNotExist
}

func (fs *NavidromeFS) OpenFile(name string, flag int, perm os.FileMode) (gofs.File, error) {
	debugLog("OpenFile(%q, 0x%x, %v)", name, flag, perm)
	parts := parsePathParts(name)
	ctx := context.Background()

	// Root - List artists folder (\)
	if len(parts) == 0 {
		artists, err := fs.client.getArtists(ctx)
		if err != nil {
			return nil, err
		}
		entries := make([]os.FileInfo, len(artists))
		for i, a := range artists {
			entries[i] = &navFileInfo{name: displayNameWithID(a.Name, a.ID), isDir: true, modTime: fs.startTime}
		}
		return &navDir{entries: entries, info: &navFileInfo{name: "", isDir: true, modTime: fs.startTime}}, nil
	}

	if len(parts) > 3 {
		return nil, os.ErrNotExist
	}

	artistID, ok := splitIDName(parts[0])
	if !ok {
		return nil, os.ErrNotExist
	}

	// Artist folder - List albums (\<artistID>\)
	if len(parts) == 1 {
		albums, err := fs.client.getAlbums(ctx, artistID)
		if err != nil {
			return nil, err
		}
		entries := make([]os.FileInfo, len(albums))
		for i, al := range albums {
			entries[i] = &navFileInfo{name: displayNameWithID(al.Name, al.ID), isDir: true, modTime: parseTime(al.Created)}
		}
		return &navDir{entries: entries, info: &navFileInfo{name: parts[0], isDir: true, modTime: fs.startTime}}, nil
	}

	albumID, ok := splitIDName(parts[1])
	if !ok {
		return nil, os.ErrNotExist
	}

	// Album folder - List songs and cover art (\<artistID>\<albumID>\)
	if len(parts) == 2 {
		album, err := fs.client.getAlbum(ctx, albumID)
		if err != nil {
			return nil, err
		}
		if !albumBelongsToArtist(album, artistID) {
			return nil, os.ErrNotExist
		}
		multiDisc := isMultiDisc(album.Songs)
		entries := make([]os.FileInfo, 0, len(album.Songs)+1)
		for _, s := range album.Songs {
			entries = append(entries, &navFileInfo{
				name:    songFileName(s, multiDisc),
				size:    s.Size,
				modTime: parseTime(s.Created),
				isDir:   false,
			})
		}
		entries = append(entries, &navFileInfo{
			name:    CoverArtName,
			size:    fs.coverSize(ctx, albumID),
			modTime: fs.startTime,
			isDir:   false,
		})
		return &navDir{entries: entries, info: &navFileInfo{
			name:    displayNameWithID(album.Name, album.ID),
			isDir:   true,
			modTime: parseTime(album.Created),
		}}, nil
	}

	// Cover file content (stream from Navidrome API or cache)
	if parts[2] == CoverArtName {
		album, err := fs.client.getAlbum(ctx, albumID)
		if err != nil {
			return nil, err
		}
		if !albumBelongsToArtist(album, artistID) {
			return nil, os.ErrNotExist
		}
		return fs.openCover(ctx, albumID)
	}

	// Song file content (stream from Navidrome API or cache)
	songID, _, ok := splitSongFileName(parts[2])
	if !ok {
		return nil, os.ErrNotExist
	}
	album, err := fs.client.getAlbum(ctx, albumID)
	if err != nil {
		return nil, err
	}
	if !albumBelongsToArtist(album, artistID) {
		return nil, os.ErrNotExist
	}
	multiDisc := isMultiDisc(album.Songs)
	for _, s := range album.Songs {
		if s.ID == songID {
			return fs.openSong(ctx, s, multiDisc)
		}
	}

	return nil, os.ErrNotExist
}

func albumBelongsToArtist(album *albumDetail, artistID string) bool {
	return album.ArtistID == "" || album.ArtistID == artistID
}

func (fs *NavidromeFS) openCover(ctx context.Context, albumID string) (gofs.File, error) {
	// Check in-memory cache first (avoid re-downloading on every OpenFile).
	if data, ok := fs.readCoverMemCache(albumID); ok {
		return &navMemFile{
			reader: bytes.NewReader(data),
			info:   &navFileInfo{name: CoverArtName, size: int64(len(data)), modTime: fs.startTime, isDir: false},
		}, nil
	}

	if fs.coverCacheDir != "" {
		if data, ok := fs.readCoverCache(albumID); ok {
			fs.writeCoverMemCache(albumID, data)
			return &navMemFile{
				reader: bytes.NewReader(data),
				info:   &navFileInfo{name: CoverArtName, size: int64(len(data)), modTime: fs.startTime, isDir: false},
			}, nil
		}
	}
	rc, err := fs.client.getCoverArt(ctx, albumID)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("reading cover art: %w", err)
	}
	fs.writeCoverMemCache(albumID, data)
	if fs.coverCacheDir != "" {
		fs.writeCoverCache(albumID, data)
	}
	return &navMemFile{
		reader: bytes.NewReader(data),
		info:   &navFileInfo{name: CoverArtName, size: int64(len(data)), modTime: fs.startTime, isDir: false},
	}, nil
}

func (fs *NavidromeFS) openSong(ctx context.Context, s song, multiDisc bool) (gofs.File, error) {
	if fs.cacheDir != "" {
		if cachedSize, _, cachedCreated, ok := ReadSongMeta(s.ID, fs.cacheDir); ok {
			if cachedSize != s.Size || cachedCreated != s.Created {
				log.Printf("Cache stale for %s, re-fetching", s.ID)
				deleteCachedFiles(s.ID, fs.cacheDir)
			}
		}
		SaveSongMeta(s.ID, s.Size, s.ContentType, s.Created, fs.cacheDir)
	}

	return &navSongFile{
		readSeeker: &readSeeker{
			ctx:           ctx,
			client:        fs.client,
			songID:        s.ID,
			cacheDir:      fs.cacheDir,
			fileSize:      s.Size,
			chunkMemCache: &fs.chunkMemCache,
		},
		info: &navFileInfo{name: songFileName(s, multiDisc), size: s.Size, modTime: parseTime(s.Created)},
	}, nil
}

func (fs *NavidromeFS) Mkdir(name string, perm os.FileMode) error {
	debugLog("Mkdir(%q, %v) -> EPERM", name, perm)
	return errReadOnly
}

func (fs *NavidromeFS) Rename(source, target string) error {
	debugLog("Rename(%q, %q) -> EPERM", source, target)
	return errReadOnly
}

func (fs *NavidromeFS) Remove(name string) error {
	debugLog("Remove(%q) -> EPERM", name)
	return errReadOnly
}

var _ gofs.FileSystem = (*NavidromeFS)(nil)
