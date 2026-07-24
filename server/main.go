package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"

	"github.com/joho/godotenv"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/winfsp/go-winfsp"
	"github.com/winfsp/go-winfsp/gofs"
	"golang.org/x/sys/windows"
)

var (
	version = "dev"
	commit  = "none"
)

func main() {
	// No Window has to be very quick, so we check argument here first
	for _, arg := range os.Args[1:] {
		if arg == "--no-window" {
			hideConsoleWindow()
			break
		}
	}

	// --stop: kill all running navifsp.exe processes
	for _, arg := range os.Args[1:] {
		if arg == "--stop" {
			cmd := exec.Command("taskkill", "/IM", "navifsp.exe", "/F")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Println("No running NaviFSP process found.")
			} else {
				fmt.Println("NaviFSP stopped.")
			}
			os.Exit(0)
		}
	}

	loadEnv()

	if os.Getenv("NAVIFSP_NO_WINDOW") == "true" {
		hideConsoleWindow()
	}

	pflag.CommandLine.SortFlags = false

	pflag.String("base-url", "", "Navidrome base URL (e.g. https://yournavidrome.com)")
	pflag.String("username", "", "Navidrome username")
	pflag.String("password", "", "Navidrome password")

	pflag.String("mount", "N:", "drive letter to mount")

	pflag.Bool("enable-music-cache", false, "enable disk cache")
	pflag.Bool("enable-cover-cache", false, "enable cover art cache (disk-based)")
	pflag.String("cache-dir", filepath.Join(os.Getenv("LOCALAPPDATA"), "navifsp", "cache"), "disk cache directory")
	pflag.Bool("verbose", false, "enable debug logging")
	pflag.Bool("no-window", false, "hide console window")
	pflag.Bool("startup-at-login", false, "auto-start on Windows login (with --no-window)")
	pflag.Usage = func() {
		fmt.Printf(`NaviFSP %s

Usage: navifsp --base-url <URL> --username <U> --password <P> [options]

Commands:
  --stop    Stop all running NaviFSP processes

Options:
`, version)
		pflag.PrintDefaults()
		fmt.Printf(`
Alternatively, you can use a .env file to configure, so the password won't end up in bash history.

Environment variables:
  NAVIFSP_BASE_URL      Navidrome base URL
  NAVIFSP_USERNAME      Navidrome username
  NAVIFSP_PASSWORD      Navidrome password
  NAVIFSP_MOUNT         Mount point path
  NAVIFSP_ENABLE_MUSIC_CACHE  Enable disk cache
  NAVIFSP_ENABLE_COVER_CACHE Enable in-memory cover art cache
  NAVIFSP_CACHE_DIR     Disk cache directory
`)
	}
	pflag.Parse()

	viper.SetEnvPrefix("NAVIFSP")
	viper.AutomaticEnv()
	viper.BindPFlags(pflag.CommandLine)
	viper.BindEnv("base-url", "NAVIFSP_BASE_URL")
	viper.BindEnv("enable-music-cache", "NAVIFSP_ENABLE_MUSIC_CACHE")
	viper.BindEnv("enable-cover-cache", "NAVIFSP_ENABLE_COVER_CACHE")
	viper.BindEnv("cache-dir", "NAVIFSP_CACHE_DIR")
	viper.BindEnv("startup-at-login", "NAVIFSP_STARTUP_AT_LOGIN")
	viper.BindEnv("no-window", "NAVIFSP_NO_WINDOW")

	syncStartup(viper.GetBool("startup-at-login"))

	baseURL := viper.GetString("base-url")
	username := viper.GetString("username")
	password := viper.GetString("password")
	mountpoint := viper.GetString("mount")
	enableCache := viper.GetBool("enable-music-cache")

	if baseURL == "" || username == "" || password == "" {
		log.Fatal("Missing required: --username, --password, --base-url (or NAVIFSP_USERNAME, NAVIFSP_PASSWORD, NAVIFSP_BASE_URL)")
	}

	client := newSubsonicClient(baseURL, username, password)
	cacheDir := viper.GetString("cache-dir")
	enableVerbose := viper.GetBool("verbose")
	verbose = enableVerbose
	enableCoverCache := viper.GetBool("enable-cover-cache")

	var musicCacheDir string
	var coverCacheDir string
	if enableCache {
		musicCacheDir = filepath.Join(cacheDir, "music")
		log.Printf("Disk cache enabled (dir: %s)", musicCacheDir)
	} else {
		log.Print("Disk cache disabled")
	}
	if enableCoverCache {
		coverCacheDir = filepath.Join(cacheDir, "cover")
		log.Printf("Cover cache enabled (dir: %s)", coverCacheDir)
	}
	navFS := NewNavidromeFS(client, musicCacheDir, coverCacheDir)

	fs, err := winfsp.Mount(gofs.New(navFS), mountpoint,
		winfsp.FileSystemName("Navidrome"),
	)
	if err != nil {
		log.Fatalf("Failed to mount filesystem: %v", err)
	}
	defer fs.Unmount()

	log.Printf("Mounted Navidrome filesystem at %s", mountpoint)

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	<-ch

	log.Println("Unmounting...")
}

func loadEnv() {
	exePath, err := os.Executable()
	if err == nil {
		envPath := filepath.Join(filepath.Dir(exePath), ".env")
		if _, statErr := os.Stat(envPath); statErr == nil {
			godotenv.Load(envPath)
			return
		}
	}
	godotenv.Load()
}

func hideConsoleWindow() {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	getConsoleWindow := kernel32.NewProc("GetConsoleWindow")
	user32 := windows.NewLazySystemDLL("user32.dll")
	showWindow := user32.NewProc("ShowWindow")

	hwnd, _, _ := getConsoleWindow.Call()
	if hwnd != 0 {
		showWindow.Call(hwnd, 0) // SW_HIDE
	}
}
