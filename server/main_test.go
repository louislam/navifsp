package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
)

type testMount struct {
	mountDir  string
	cmd       *exec.Cmd
	exited    chan struct{}
	artistDir string
	albumDir  string
	albumPath string
	songName  string
}

var tm *testMount

func TestMain(m *testing.M) {
	godotenv.Load("../.env")
	baseURL := os.Getenv("NAVIFSP_BASE_URL")
	username := os.Getenv("NAVIFSP_USERNAME")
	password := os.Getenv("NAVIFSP_PASSWORD")
	if baseURL == "" || username == "" || password == "" {
		fmt.Fprintln(os.Stderr, "Set NAVIFSP_BASE_URL, NAVIFSP_USERNAME, NAVIFSP_PASSWORD")
		os.Exit(1)
	}

	pkgDir, err := filepath.Abs(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	exePath := filepath.Join(pkgDir, "../navifsp_test.exe")
	mountDir := "P:"

	build := exec.Command("go", "build", "-o", exePath, ".")
	build.Dir = pkgDir
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build failed: %v\n%s\n", err, out)
		os.Exit(1)
	}

	os.MkdirAll(filepath.Dir(mountDir), 0o755)
	os.RemoveAll(mountDir)

	ctx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, exePath,
		"--base-url", baseURL,
		"--username", username,
		"--password", password,
		"--mount", mountDir,
	)
	cmd.Dir = pkgDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start: %v\n", err)
		os.Exit(1)
	}

	exited := make(chan struct{})
	go func() { cmd.Wait(); close(exited) }()

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-exited:
			fmt.Fprintln(os.Stderr, "process exited before mount was ready")
			os.Exit(1)
		default:
		}
		entries, err := os.ReadDir(mountDir)
		if err == nil && len(entries) > 0 {
			log.Printf("Mount ready, root has %d entries", len(entries))
			break
		}
		time.Sleep(1 * time.Second)
	}
	time.Sleep(3 * time.Second)

	tm = &testMount{mountDir: mountDir, cmd: cmd, exited: exited}

	artists, err := os.ReadDir(mountDir)
	if err != nil || len(artists) == 0 {
		fmt.Fprintln(os.Stderr, "no artists available")
		os.Exit(1)
	}
	tm.artistDir = artists[0].Name()

	albumEntries, err := os.ReadDir(filepath.Join(mountDir, tm.artistDir))
	if err != nil || len(albumEntries) == 0 {
		fmt.Fprintln(os.Stderr, "no albums available")
		os.Exit(1)
	}
	tm.albumDir = albumEntries[0].Name()
	tm.albumPath = filepath.Join(mountDir, tm.artistDir, tm.albumDir)

	songs, err := os.ReadDir(tm.albumPath)
	if err == nil {
		for _, s := range songs {
			if !s.IsDir() && s.Name() != CoverArtName {
				tm.songName = s.Name()
				break
			}
		}
	}

	code := m.Run()

	cancel()
	if cmd.Process != nil {
		exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprint(cmd.Process.Pid)).Run()
	}
	os.Exit(code)
}

func TestMountIntegration(t *testing.T) {
	artists, err := os.ReadDir(tm.mountDir)
	if err != nil {
		t.Fatalf("failed to read root: %v", err)
	}
	t.Logf("Got %d artists", len(artists))
	t.Logf("Using artist: %q", tm.artistDir)

	artistPath := filepath.Join(tm.mountDir, tm.artistDir)
	albums, err := os.ReadDir(artistPath)
	if err != nil {
		t.Fatalf("failed to read artist dir: %v", err)
	}
	t.Logf("Got %d albums for %q", len(albums), tm.artistDir)
	t.Logf("Using album: %q", tm.albumDir)

	songs, err := os.ReadDir(tm.albumPath)
	if err != nil {
		t.Fatalf("failed to read album dir: %v", err)
	}
	t.Logf("Got %d entries for %q", len(songs), tm.albumDir)

	hasCover := false
	for _, s := range songs {
		if s.Name() == CoverArtName {
			hasCover = true
		}
	}

	if !hasCover {
		t.Fatal(CoverArtName + " not found in album listing")
	}
	coverPath := filepath.Join(tm.albumPath, CoverArtName)
	coverInfo, err := os.Stat(coverPath)
	if err != nil {
		t.Fatalf("failed to stat cover art: %v", err)
	}
	t.Logf("Cover art: %d bytes", coverInfo.Size())

	coverData, err := os.ReadFile(coverPath)
	if err != nil {
		t.Fatalf("failed to read cover art: %v", err)
	}
	if len(coverData) < 100 {
		t.Fatalf("cover art too small: %d bytes", len(coverData))
	}
	if coverData[0] != 0xff || coverData[1] != 0xd8 {
		t.Fatalf("cover art is not JPEG, header: %x", coverData[:4])
	}
	t.Logf("Cover art: valid JPEG, %d bytes", len(coverData))

	if int64(len(coverData)) != coverInfo.Size() {
		t.Fatalf("cover art stat size %d does not match actual read size %d", coverInfo.Size(), len(coverData))
	}
	t.Logf("Cover art stat size matches actual size: %d bytes", coverInfo.Size())

	if tm.songName == "" {
		t.Fatal("no song file found")
	}
	songPath := filepath.Join(tm.albumPath, tm.songName)
	songInfo, err := os.Stat(songPath)
	if err != nil {
		t.Fatalf("failed to stat song: %v", err)
	}
	t.Logf("Song: %q, %d bytes", tm.songName, songInfo.Size())

	f, err := os.Open(songPath)
	if err != nil {
		t.Fatalf("failed to open song: %v", err)
	}
	defer f.Close()

	buf := make([]byte, 1024)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		t.Fatalf("failed to read song: %v", err)
	}
	t.Logf("Read first %d bytes of %q", n, tm.songName)

	ext := strings.ToLower(filepath.Ext(tm.songName))
	if ext == ".flac" && string(buf[:4]) == "fLaC" {
		t.Log("Detected FLAC header")
	} else if ext == ".mp3" && string(buf[:3]) == "ID3" {
		t.Log("Detected MP3 header")
	} else {
		t.Logf("Header: %x", buf[:4])
	}
}

func TestWriteOperations(t *testing.T) {
	t.Run("CreateFile", func(t *testing.T) {
		f, err := os.Create(filepath.Join(tm.albumPath, "test.txt"))
		if err == nil {
			f.Close()
			t.Fatal("expected error creating file, got nil")
		}
		t.Logf("CreateFile correctly failed: %v", err)
	})

	t.Run("WriteExistingFile", func(t *testing.T) {
		if tm.songName == "" {
			t.Skip("no songs to test write on")
		}
		f, err := os.OpenFile(filepath.Join(tm.albumPath, tm.songName), os.O_WRONLY, 0)
		if err == nil {
			_, werr := f.Write([]byte("test"))
			f.Close()
			if werr == nil {
				t.Fatal("expected error writing to file, got nil")
			}
			t.Logf("Write correctly failed: %v", werr)
		} else {
			t.Logf("OpenFile for write correctly failed: %v", err)
		}
	})

	t.Run("CreateDirectory", func(t *testing.T) {
		err := os.Mkdir(filepath.Join(tm.albumPath, "newdir"), 0o755)
		if err == nil {
			t.Fatal("expected error creating directory, got nil")
		}
		t.Logf("Mkdir correctly failed: %v", err)
	})

	t.Run("RemoveFile", func(t *testing.T) {
		if tm.songName == "" {
			t.Skip("no songs to test remove on")
		}
		err := os.Remove(filepath.Join(tm.albumPath, tm.songName))
		if err == nil {
			t.Fatal("expected error removing file, got nil")
		}
		t.Logf("Remove correctly failed: %v", err)
	})

	t.Run("RenameFile", func(t *testing.T) {
		if tm.songName == "" {
			t.Skip("no songs to test rename on")
		}
		err := os.Rename(
			filepath.Join(tm.albumPath, tm.songName),
			filepath.Join(tm.albumPath, "renamed"+filepath.Ext(tm.songName)),
		)
		if err == nil {
			t.Fatal("expected error renaming file, got nil")
		}
		t.Logf("Rename correctly failed: %v", err)
	})

	t.Run("StatNonexistent", func(t *testing.T) {
		_, err := os.Stat(filepath.Join(tm.albumPath, "doesnotexist.txt"))
		if err == nil {
			t.Fatal("expected error stat-ing nonexistent file, got nil")
		}
		t.Logf("Stat nonexistent correctly failed: %v", err)
	})
}
