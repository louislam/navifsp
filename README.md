# NaviFSP

Mounts a Navidrome server as a drive or a folder. Allowing you to add your Navidrome music library to Foobar2000 on Windows.

It should also work with other music players.

## Features

- Expose all music as normal files (*.mp3, *.flac, etc.)
- Expose album covers as `cover.jpg`
- Supports caching
- Save bandwidth when scanning all files - only reads a small chunk of the song data to get the metadata.

## Requirements

- Windows 10 or 11 (x64)
- WinFSP ([Download](https://github.com/winfsp/winfsp/releases/latest))
- foobar2000 (or other music players)

## Basic usage

1. Download the latest release from https://github.com/louislam/navifsp/releases/latest.
2. Unzip
3. Open `.env` file and set your Navidrome `username`, `password`, and your `Navidrome base URL`.
4. Make sure you actually have WinFSP installed.
5. Run `navifsp.exe` to mount your Navidrome music library, by default it will mount to `N:\` drive.
6. Done.

```bash
navifsp.exe --username abc --password 123 --base-url https://yournavidrome.com --mount N:
```

### Flags and Environment Variables

| Flag                   | Env Var                      | Default                       | Required | Description                                                   |
|------------------------|------------------------------|-------------------------------|----------|---------------------------------------------------------------|
| `--username`           | `NAVIFSP_USERNAME`           |                               | Yes      | Navidrome username                                            |
| `--password`           | `NAVIFSP_PASSWORD`           |                               | Yes      | Navidrome password                                            |
| `--base-url`           | `NAVIFSP_BASE_URL`           |                               | Yes      | Navidrome base URL (e.g. https://yournavidrome.com)           |
| `--mount`              | `NAVIFSP_MOUNT`              | `N:`                          | No       | Drive letter or folder path to mount (`N:` or `C:\navidrome`) |
| `--enable-music-cache` | `NAVIFSP_ENABLE_MUSIC_CACHE` | `false`                       | No       | Enable disk cache for song data                               |
| `--enable-cover-cache` | `NAVIFSP_ENABLE_COVER_CACHE` | `false`                       | No       | Enable disk-based cover art cache                             |
| `--cache-dir`          | `NAVIFSP_CACHE_DIR`          | `AppData\Local\navifsp\cache` | No       | Disk cache directory (only used when cache enabled)           |
| `--verbose`            | `NAVIFSP_VERBOSE`            | `false`                       | No       | Enable debug logging                                          |
| `--no-window`          | `NAVIFSP_NO_WINDOW`          | `false`                       | No       | Hide console window                                           |
| `--startup-at-login`   | `NAVIFSP_STARTUP_AT_LOGIN`   | `false`                       | No       | Auto-start on Windows login (with `--no-window`)             |

Environment variables can also be set via a `.env` file next to the exe file.

### Add to Startup

1. You must have to use `.env` file to configure, flags are not supported.
2. NAVIFSP_NO_WINDOW=true
3. NAVIFSP_STARTUP_AT_LOGIN=true
4. Run `navifsp.exe`, now it should be running in the background, also next time you login, it will auto start.

## Q&A

### Why are music file created/modified datetime wrong?

By default, Navidrome does not return the actual time.

Set `ND_RECENTLYADDEDBYMODTIME=true` on your Navidrome server to use the actual file modification time instead.

### Why are some artist folders empty?

To avoid duplicate albums, NaviFSP only shows albums where the artist is the primary album artist. Artists who only appear as co-artists or guest performers will have empty folders.

### Why are the folder and filenames random IDs instead of artist/album/song names?

Using real names would require sanitizing characters that are illegal in Windows file paths, which can create collisions. I don't want to dig into it.

And this project is not meant to be used in File Explorer, after added to Foobar2000, everything will be well organized and displayed in Foobar2000.


## Motivation

As a very long time user of Foobar2000, I honestly can't adopt to any existing Navidrom Desktop clients. Most of them are very spotify-like, and missing the professional features of Foobar2000.

## Just Some Sharing

Before I finally decided to write using WinFSP, the project was originally developed using TypeScript and WebDAV, and surprisingly, using WebDAV is very bad desicion:

- Desipte the fact that Foobar2000 supports WebDAV natively, it can't show the album covers for unknown reason.
- Windows is able to map WebDAV as a network drive, and some files are missing for unknown reason.
- Node.js or Deno is really slow comparing to Golang, even though it looks like I have written everything in async, blocking I/O still occurs for unknown reason.

## Possible Future Features?

- Music transcoding? (File size issue, can't get the full file size without encoding the whole file?)
- Add an option to use artist/album/track names instead of IDs in the file paths (but it may have characters that are not allowed in Windows file paths)
- Ability to play cached music offline, cache whole folder structure?
- Playlists?
- Foobar2000 component?

## Related Projects

- foo_opensubsonic (https://github.com/michioxd/foo_opensubsonic)

If you are fine with your songs only added to playlist, but not added to your music library, it should be a simpler solution.
