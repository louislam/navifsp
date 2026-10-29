package main

import (
	"encoding/binary"
	"testing"
)

func TestIconImageIsDIB(t *testing.T) {
	img, ok := iconImage(iconICO)
	if !ok {
		t.Fatal("no uncompressed image found in the embedded icon")
	}
	// A DIB icon image starts with its BITMAPINFOHEADER.
	if got := binary.LittleEndian.Uint32(img[0:4]); got != 40 {
		t.Fatalf("header size = %d, want 40", got)
	}
	if w := binary.LittleEndian.Uint32(img[4:8]); w != 32 {
		t.Fatalf("width = %d, want 32", w)
	}
}

// TestIconHasAllSizes guards the .ico the shell shows in Explorer, which needs
// more than the one image the tray uses.
func TestIconHasAllSizes(t *testing.T) {
	if got := binary.LittleEndian.Uint16(iconICO[2:4]); got != 1 {
		t.Fatalf("icon type = %d, want 1", got)
	}
	count := int(binary.LittleEndian.Uint16(iconICO[4:6]))
	found := map[int]bool{}
	for i := 0; i < count; i++ {
		off := 6 + i*16
		w := int(iconICO[off])
		if w == 0 {
			w = 256
		}
		found[w] = true
	}
	for _, px := range []int{16, 32, 48, 256} {
		if !found[px] {
			t.Errorf("icon.ico has no %dx%d image", px, px)
		}
	}
}
