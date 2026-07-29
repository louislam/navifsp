package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/winfsp/go-winfsp/gofs"
)

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
		return time.Now()
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05", s)
	}
	if err != nil {
		return time.Now()
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

func (fs *NavidromeFS) Stat(name string) (os.FileInfo, error) {
	debugLog("Stat(%q)", name)
	parts := parsePathParts(name)

	if len(parts) == 0 {
		return &navFileInfo{
			name:    "",
			isDir:   true,
			modTime: fs.startTime}, nil
	}

	if len(parts) == 1 {
		artists, err := fs.client.getArtists(context.Background())
		if err != nil {
			return nil, err
		}
		for _, a := range artists {
			if a.ID == parts[0] {
				return &navFileInfo{name: a.ID, isDir: true, modTime: time.Now()}, nil
			}
		}
		return nil, os.ErrNotExist
	}

	if len(parts) == 2 {
		albums, err := fs.client.getAlbums(context.Background(), parts[0])
		if err != nil {
			return nil, err
		}
		for _, al := range albums {
			if al.ID == parts[1] {
				return &navFileInfo{name: al.ID, isDir: true, modTime: parseTime(al.Created)}, nil
			}
		}
		return nil, os.ErrNotExist
	}

	if len(parts) == 3 && parts[2] == CoverArtName {
		albumID := parts[1]
		albums, err := fs.client.getAlbums(context.Background(), parts[0])
		if err != nil {
			return nil, err
		}
		for _, al := range albums {
			if al.ID == albumID {
				var size int64
				if cached, ok := fs.coverMemCache.Load(albumID); ok {
					size = int64(len(cached.([]byte)))
				} else if fs.coverCacheDir != "" {
					if data, ok := fs.readCoverCache(albumID); ok {
						fs.coverMemCache.Store(albumID, data)
						size = int64(len(data))
					} else {
						size, _ = fs.client.getCoverArtSize(context.Background(), albumID)
					}
				} else {
					size, _ = fs.client.getCoverArtSize(context.Background(), albumID)
				}
				return &navFileInfo{name: CoverArtName, size: size, modTime: fs.startTime, isDir: false}, nil
			}
		}
		return nil, os.ErrNotExist
	}

	if len(parts) == 3 {
		songID := strings.TrimSuffix(parts[2], path.Ext(parts[2]))

		if size, _, created, ok := ReadSongMeta(songID, fs.cacheDir); ok && fs.cacheDir != "" {
			apiSong, err := fs.client.getSong(context.Background(), songID)
			if err == nil && (apiSong.Size != size || apiSong.Created != created) {
				log.Printf("Cache stale for %s, re-fetching", songID)
				deleteCachedFiles(songID, fs.cacheDir)
				SaveSongMeta(songID, apiSong.Size, apiSong.ContentType, apiSong.Created, fs.cacheDir)
				return &navFileInfo{name: parts[2], size: apiSong.Size, modTime: parseTime(apiSong.Created), isDir: false}, nil
			}
			if err == nil {
				return &navFileInfo{name: parts[2], size: size, modTime: parseTime(created), isDir: false}, nil
			}
		}

		songs, err := fs.client.getSongs(context.Background(), parts[1])
		if err != nil {
			return nil, err
		}
		for _, s := range songs {
			if s.ID == songID {
				if fs.cacheDir != "" {
					SaveSongMeta(songID, s.Size, s.ContentType, s.Created, fs.cacheDir)
				}
				return &navFileInfo{name: parts[2], size: s.Size, modTime: parseTime(s.Created), isDir: false}, nil
			}
		}
		return nil, os.ErrNotExist
	}

	return nil, os.ErrNotExist
}

func (fs *NavidromeFS) OpenFile(name string, flag int, perm os.FileMode) (gofs.File, error) {
	debugLog("OpenFile(%q, 0x%x, %v)", name, flag, perm)
	parts := parsePathParts(name)

	// Root - List artists folder (\)
	if len(parts) == 0 {
		artists, err := fs.client.getArtists(context.Background())
		if err != nil {
			return nil, err
		}
		entries := make([]os.FileInfo, len(artists))
		for i, a := range artists {
			entries[i] = &navFileInfo{name: a.ID, isDir: true, modTime: time.Now()}
		}
		return &navDir{entries: entries, info: &navFileInfo{name: "", isDir: true, modTime: fs.startTime}}, nil
	}

	// Artist folder - List albums (\<artistID>\)
	if len(parts) == 1 {
		albums, err := fs.client.getAlbums(context.Background(), parts[0])
		if err != nil {
			return nil, err
		}
		entries := make([]os.FileInfo, len(albums))
		for i, al := range albums {
			entries[i] = &navFileInfo{name: al.ID, isDir: true, modTime: parseTime(al.Created)}
		}
		return &navDir{entries: entries, info: &navFileInfo{name: parts[0], isDir: true, modTime: time.Now()}}, nil
	}

	// Album folder - List songs and cover art (\<artistID>\<albumID>\)
	if len(parts) == 2 {
		songs, err := fs.client.getSongs(context.Background(), parts[1])
		if err != nil {
			return nil, err
		}
		entries := make([]os.FileInfo, 0, len(songs)+1)
		for _, s := range songs {
			ext := ""
			if dot := strings.LastIndex(s.Path, "."); dot >= 0 {
				ext = s.Path[dot:]
			}
			fileName := s.ID + ext
			entries = append(entries, &navFileInfo{name: fileName, size: s.Size, modTime: parseTime(s.Created), isDir: false})
		}
		var coverSize int64
		albumID := parts[1]
		if cached, ok := fs.coverMemCache.Load(albumID); ok {
			coverSize = int64(len(cached.([]byte)))
		} else if fs.coverCacheDir != "" {
			if data, ok := fs.readCoverCache(albumID); ok {
				fs.coverMemCache.Store(albumID, data)
				coverSize = int64(len(data))
			} else {
				rc, err := fs.client.getCoverArt(context.Background(), albumID)
				if err == nil {
					data, readErr := io.ReadAll(rc)
					rc.Close()
					if readErr == nil {
						fs.writeCoverCache(albumID, data)
						fs.coverMemCache.Store(albumID, data)
						coverSize = int64(len(data))
					}
				}
			}
		} else {
			rc, err := fs.client.getCoverArt(context.Background(), albumID)
			if err == nil {
				data, readErr := io.ReadAll(rc)
				rc.Close()
				if readErr == nil {
					fs.coverMemCache.Store(albumID, data)
					coverSize = int64(len(data))
				}
			}
		}
		entries = append(entries, &navFileInfo{name: CoverArtName, size: coverSize, modTime: fs.startTime, isDir: false})
		return &navDir{entries: entries, info: &navFileInfo{name: parts[1], isDir: true, modTime: time.Now()}}, nil
	}

	// Cover file content (stream from Navidrome API or cache) (\<artistID>\<albumID>\cover.jpg)
	if len(parts) == 3 && parts[2] == CoverArtName {
		albumID := parts[1]

		// Check in-memory cache first (avoid re-downloading on every OpenFile).
		if cached, ok := fs.coverMemCache.Load(albumID); ok {
			data := cached.([]byte)
			return &navMemFile{
				reader: bytes.NewReader(data),
				info:   &navFileInfo{name: CoverArtName, size: int64(len(data)), modTime: fs.startTime, isDir: false},
			}, nil
		}

		if fs.coverCacheDir != "" {
			if data, ok := fs.readCoverCache(albumID); ok {
				fs.coverMemCache.Store(albumID, data)
				return &navMemFile{
					reader: bytes.NewReader(data),
					info:   &navFileInfo{name: CoverArtName, size: int64(len(data)), modTime: fs.startTime, isDir: false},
				}, nil
			}
		}
		rc, err := fs.client.getCoverArt(context.Background(), albumID)
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			return nil, fmt.Errorf("reading cover art: %w", err)
		}
		fs.coverMemCache.Store(albumID, data)
		if fs.coverCacheDir != "" {
			fs.writeCoverCache(albumID, data)
		}
		return &navMemFile{
			reader: bytes.NewReader(data),
			info:   &navFileInfo{name: CoverArtName, size: int64(len(data)), modTime: fs.startTime, isDir: false},
		}, nil
	}

	// Song file content (stream from Navidrome API or cache) (\<artistID>\<albumID>\<songID>.<ext>)
	if len(parts) == 3 {
		songID := strings.TrimSuffix(parts[2], path.Ext(parts[2]))
		ctx := context.Background()

		size, _, created, ok := ReadSongMeta(songID, fs.cacheDir)
		if ok && fs.cacheDir != "" {
			apiSong, err := fs.client.getSong(ctx, songID)
			if err == nil && (apiSong.Size != size || apiSong.Created != created) {
				log.Printf("Cache stale for %s, re-fetching", songID)
				deleteCachedFiles(songID, fs.cacheDir)
				size = apiSong.Size
				created = apiSong.Created
				ok = false
			}
			if err != nil {
				ok = false
			}
		}

		if !ok {
			songs, err := fs.client.getSongs(ctx, parts[1])
			if err != nil {
				return nil, err
			}
			for _, s := range songs {
				if s.ID == songID {
					size = s.Size
					created = s.Created
					ok = true
					if fs.cacheDir != "" {
						SaveSongMeta(songID, s.Size, s.ContentType, s.Created, fs.cacheDir)
					}
					break
				}
			}
			if !ok {
				return nil, os.ErrNotExist
			}
		}

		return &navSongFile{
			readSeeker: &readSeeker{
				ctx:      ctx,
				client:   fs.client,
				songID:   songID,
				cacheDir: fs.cacheDir,
				fileSize: size,
			},
			info: &navFileInfo{name: parts[2], size: size, modTime: parseTime(created), isDir: false},
		}, nil
	}

	return nil, os.ErrNotExist
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
