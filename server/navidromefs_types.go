package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"time"
)

const CoverArtName = "cover.jpg"

const fetchChunkSize = int64(256 * 1024)

var errReadOnly = syscall.EPERM

// navFileInfo implements os.FileInfo for virtual filesystem entries.
type navFileInfo struct {
	name    string
	size    int64
	modTime time.Time
	isDir   bool
}

func (fi *navFileInfo) Name() string       { return fi.name }
func (fi *navFileInfo) Size() int64        { return fi.size }
func (fi *navFileInfo) ModTime() time.Time { return fi.modTime }
func (fi *navFileInfo) IsDir() bool        { return fi.isDir }
func (fi *navFileInfo) Sys() any           { return nil }

func (fi *navFileInfo) Mode() os.FileMode {
	if fi.isDir {
		return os.ModeDir | 0555
	}
	return 0444
}

// readSeeker fetches song data in chunks via range requests.
type readSeeker struct {
	ctx           context.Context
	client        *subsonicClient
	songID        string
	cacheDir      string
	fileSize      int64
	pos           int64
	chunkMemCache *sync.Map
}

func (r *readSeeker) Read(p []byte) (int, error) {
	if r.pos >= r.fileSize {
		return 0, io.EOF
	}

	end := r.pos + int64(len(p))
	if end > r.fileSize {
		end = r.fileSize
	}

	chunkStart := r.pos - (r.pos % fetchChunkSize)
	chunkEnd := chunkStart + fetchChunkSize - 1
	if chunkEnd >= r.fileSize {
		chunkEnd = r.fileSize - 1
	}

	if r.cacheDir != "" && chunkStart >= 0 {
		cached, err := ReadCachedRange(r.songID, int(chunkStart), int(chunkEnd), r.cacheDir)
		if err == nil && cached != nil {
			off := int(r.pos - chunkStart)
			n := int(end - r.pos)
			if off+n > len(cached) {
				n = len(cached) - off
			}
			if n > 0 {
				copy(p, cached[off:off+n])
				r.pos += int64(n)
				return n, nil
			}
		}
	}

	if data, ok := r.readChunkFromMem(chunkStart); ok {
		off := int(r.pos - chunkStart)
		n := int(end - r.pos)
		if off+n > len(data) {
			n = len(data) - off
		}
		if n > 0 {
			copy(p, data[off:off+n])
			r.pos += int64(n)
			return n, nil
		}
	}

	debugLog("Fetch %s %d-%d", r.songID, chunkStart, chunkEnd)
	body, err := r.client.streamSongRange(r.ctx, r.songID, chunkStart, chunkEnd)
	if err != nil {
		return 0, fmt.Errorf("fetching range: %w", err)
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		return 0, fmt.Errorf("reading range: %w", err)
	}

	r.writeChunkToMem(chunkStart, data)
	if r.cacheDir != "" {
		CacheChunk(r.songID, int(chunkStart), data, r.cacheDir)
	}

	off := r.pos - chunkStart
	n := int(end - r.pos)
	if off+int64(n) > int64(len(data)) {
		n = int(int64(len(data)) - off)
	}
	if n <= 0 {
		return 0, io.EOF
	}
	copy(p, data[off:off+int64(n)])
	r.pos += int64(n)
	return n, nil
}

func (r *readSeeker) ReadAt(p []byte, off int64) (int, error) {
	if off >= r.fileSize {
		return 0, io.EOF
	}

	end := off + int64(len(p))
	if end > r.fileSize {
		end = r.fileSize
	}

	chunkStart := off - (off % fetchChunkSize)
	chunkEnd := chunkStart + fetchChunkSize - 1
	if chunkEnd >= r.fileSize {
		chunkEnd = r.fileSize - 1
	}

	if r.cacheDir != "" && chunkStart >= 0 {
		cached, err := ReadCachedRange(r.songID, int(chunkStart), int(chunkEnd), r.cacheDir)
		if err == nil && cached != nil {
			relOff := int(off - chunkStart)
			n := int(end - off)
			if relOff+n > len(cached) {
				n = len(cached) - relOff
			}
			if n > 0 {
				copy(p, cached[relOff:relOff+n])
				return n, nil
			}
		}
	}

	if data, ok := r.readChunkFromMem(chunkStart); ok {
		relOff := int(off - chunkStart)
		n := int(end - off)
		if relOff+n > len(data) {
			n = len(data) - relOff
		}
		if n > 0 {
			copy(p, data[relOff:relOff+n])
			return n, nil
		}
	}

	debugLog("FetchAt %s %d-%d", r.songID, chunkStart, chunkEnd)
	body, err := r.client.streamSongRange(r.ctx, r.songID, chunkStart, chunkEnd)
	if err != nil {
		return 0, fmt.Errorf("fetching range: %w", err)
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		return 0, fmt.Errorf("reading range: %w", err)
	}

	r.writeChunkToMem(chunkStart, data)
	if r.cacheDir != "" {
		CacheChunk(r.songID, int(chunkStart), data, r.cacheDir)
	}

	relOff := off - chunkStart
	n := int(end - off)
	if relOff+int64(n) > int64(len(data)) {
		n = int(int64(len(data)) - relOff)
	}
	if n <= 0 {
		return 0, io.EOF
	}
	copy(p, data[relOff:relOff+int64(n)])
	return n, nil
}

func (r *readSeeker) readChunkFromMem(chunkStart int64) ([]byte, bool) {
	if r.chunkMemCache == nil {
		return nil, false
	}
	key := fmt.Sprintf("%s:%d", r.songID, chunkStart)
	val, ok := r.chunkMemCache.Load(key)
	if !ok {
		return nil, false
	}
	return val.([]byte), true
}

func (r *readSeeker) writeChunkToMem(chunkStart int64, data []byte) {
	if r.chunkMemCache == nil {
		return
	}
	key := fmt.Sprintf("%s:%d", r.songID, chunkStart)
	r.chunkMemCache.Store(key, data)
}

func (r *readSeeker) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		r.pos = offset
	case io.SeekCurrent:
		r.pos += offset
	case io.SeekEnd:
		r.pos = r.fileSize + offset
	}
	if r.pos < 0 {
		r.pos = 0
	}
	return r.pos, nil
}

func (r *readSeeker) Write(p []byte) (int, error)              { return 0, errReadOnly }
func (r *readSeeker) WriteAt(p []byte, off int64) (int, error) { return 0, errReadOnly }
func (r *readSeeker) Truncate(size int64) error                { return errReadOnly }
func (r *readSeeker) Sync() error                              { return nil }
func (r *readSeeker) Close() error                             { return nil }
func (r *readSeeker) Stat() (os.FileInfo, error)               { return nil, errReadOnly }
func (r *readSeeker) Readdir(count int) ([]os.FileInfo, error) { return nil, errReadOnly }

// navSongFile wraps readSeeker to implement gofs.File.
type navSongFile struct {
	*readSeeker
	info *navFileInfo
}

func (f *navSongFile) Stat() (os.FileInfo, error) {
	debugLog("songFile.Stat() on %q", f.info.name)
	return f.info, nil
}

// navMemFile implements gofs.File for in-memory data.
type navMemFile struct {
	reader *bytes.Reader
	info   *navFileInfo
}

func (f *navMemFile) Read(p []byte) (int, error)              { return f.reader.Read(p) }
func (f *navMemFile) ReadAt(p []byte, off int64) (int, error) { return f.reader.ReadAt(p, off) }
func (f *navMemFile) Seek(offset int64, whence int) (int64, error) {
	return f.reader.Seek(offset, whence)
}
func (f *navMemFile) Write(p []byte) (int, error)              { return 0, errReadOnly }
func (f *navMemFile) WriteAt(p []byte, off int64) (int, error) { return 0, errReadOnly }
func (f *navMemFile) Truncate(size int64) error                { return errReadOnly }
func (f *navMemFile) Sync() error                              { return nil }
func (f *navMemFile) Close() error                             { return nil }
func (f *navMemFile) Stat() (os.FileInfo, error) {
	debugLog("memFile.Stat() on %q", f.info.name)
	return f.info, nil
}
func (f *navMemFile) Readdir(count int) ([]os.FileInfo, error) { return nil, errReadOnly }

// navDir implements gofs.File for directories.
type navDir struct {
	entries []os.FileInfo
	pos     int
	info    *navFileInfo
}

func (d *navDir) Read(p []byte) (int, error)                   { return 0, errReadOnly }
func (d *navDir) ReadAt(p []byte, off int64) (int, error)      { return 0, errReadOnly }
func (d *navDir) Write(p []byte) (int, error)                  { return 0, errReadOnly }
func (d *navDir) WriteAt(p []byte, off int64) (int, error)     { return 0, errReadOnly }
func (d *navDir) Seek(offset int64, whence int) (int64, error) { return 0, errReadOnly }
func (d *navDir) Truncate(size int64) error                    { return errReadOnly }
func (d *navDir) Sync() error                                  { return nil }
func (d *navDir) Close() error                                 { return nil }
func (d *navDir) Stat() (os.FileInfo, error) {
	debugLog("dir.Stat() on %q", d.info.name)
	return d.info, nil
}

func (d *navDir) Readdir(count int) ([]os.FileInfo, error) {
	debugLog("Readdir(%d) on %q (pos=%d, total=%d)", count, d.info.name, d.pos, len(d.entries))
	if count <= 0 {
		result := d.entries[d.pos:]
		d.pos = len(d.entries)
		return result, nil
	}
	end := d.pos + count
	if end > len(d.entries) {
		end = len(d.entries)
	}
	result := d.entries[d.pos:end]
	d.pos = end
	return result, nil
}
