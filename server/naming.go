package main

import (
	"fmt"
	"path"
	"strings"
	"unicode/utf16"
)

// maxPathComponent bounds a generated path component so a deep
// artist/album/song path stays close to the classic Windows MAX_PATH limit
// (260 characters), which players like foobar2000 may rely on.
const maxPathComponent = 100

// songFileName builds "NN - Title [songID].ext" (or "D-NN - Title [songID].ext"
// for multi-disc albums). The ID is the identity; the display part is cosmetic.
func songFileName(s song, multiDisc bool) string {
	ext := path.Ext(s.Path)
	base := sanitizeComponent(songBaseName(s, multiDisc))
	if base == "" {
		base = sanitizeComponent(s.ID)
	}
	if base == "" {
		base = "unnamed"
	}
	suffix := " [" + s.ID + "]"
	budget := maxPathComponent - utf16Len(suffix) - utf16Len(ext)
	if budget < 1 {
		budget = 1
	}
	return truncateComponent(base, budget) + suffix + ext
}

// displayNameWithID builds "Name [id]" for artists and albums.
func displayNameWithID(display, id string) string {
	display = sanitizeComponent(display)
	if display == "" {
		display = sanitizeComponent(id)
	}
	if display == "" {
		display = "unnamed"
	}
	suffix := " [" + id + "]"
	budget := maxPathComponent - utf16Len(suffix)
	if budget < 1 {
		budget = 1
	}
	return truncateComponent(display, budget) + suffix
}

// songBaseName builds the human readable part of a song file name.
func songBaseName(s song, multiDisc bool) string {
	title := s.Title
	if title == "" {
		title = strings.TrimSuffix(path.Base(s.Path), path.Ext(s.Path))
	}
	if s.Track <= 0 {
		return title
	}
	if multiDisc {
		disc := s.DiscNumber
		if disc <= 0 {
			disc = 1
		}
		return fmt.Sprintf("%d-%02d - %s", disc, s.Track, title)
	}
	return fmt.Sprintf("%02d - %s", s.Track, title)
}

func isMultiDisc(songs []song) bool {
	for _, s := range songs {
		if s.DiscNumber > 1 {
			return true
		}
	}
	return false
}

// splitIDName extracts the trailing " [id]" from an artist or album segment.
// The ID is always the last bracketed group, so display names may contain
// brackets themselves.
func splitIDName(segment string) (string, bool) {
	start := strings.LastIndex(segment, " [")
	if start < 0 || !strings.HasSuffix(segment, "]") {
		return "", false
	}
	id := segment[start+2 : len(segment)-1]
	if !validID(id) {
		return "", false
	}
	return id, true
}

// splitSongFileName extracts the song ID and extension from
// "NN - Title [songID].ext". The last ']' closes the ID, so dots in the
// display title or the ID do not confuse extension parsing.
func splitSongFileName(segment string) (songID, ext string, ok bool) {
	close := strings.LastIndex(segment, "]")
	if close < 0 {
		return "", "", false
	}
	ext = segment[close+1:]
	if ext != "" && !strings.HasPrefix(ext, ".") {
		return "", "", false
	}
	id, ok := splitIDName(segment[:close+1])
	if !ok {
		return "", "", false
	}
	return id, ext, true
}

func validID(id string) bool {
	return id != "" && !strings.ContainsAny(id, "[]/\\")
}

var windowsReservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true,
	"COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true,
	"LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// sanitizeComponent replaces characters that Windows does not allow in a file
// or directory name and avoids reserved device names. The ID suffix is added
// afterwards and is never sanitized.
// https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-file
func sanitizeComponent(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r < 0x20 || r == 0x7f:
			b.WriteByte('_')
		case strings.ContainsRune(`<>:"/\|?*`, r):
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimRight(b.String(), " .")
	if out == "" || out == "." || out == ".." {
		return ""
	}
	base := out
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	if windowsReservedNames[strings.ToUpper(base)] {
		out = "_" + out
	}
	return out
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		l := utf16.RuneLen(r)
		if l < 1 {
			l = 1
		}
		n += l
	}
	return n
}

// truncateComponent limits a name in UTF-16 code units, the unit Windows
// uses for the 255-character per-component limit.
func truncateComponent(s string, max int) string {
	units := 0
	for i, r := range s {
		l := utf16.RuneLen(r)
		if l < 1 {
			l = 1
		}
		units += l
		if units > max {
			return s[:i]
		}
	}
	return s
}
