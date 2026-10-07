# mediabrowser

A simple web-based media browser for your local network. Run one executable,
point it at a folder, and browse it from any phone, tablet or computer on the
same network, including uploading files into it.

- Grid or list view, with thumbnails for images, videos and audio (album art, or else a waveform), and a cover of up to four photos for folders
- Full-screen viewer with swipe and arrow-key navigation, zoom and pan for photos, video streaming and seeking, and an audio player with a waveform to seek in, which plays through a folder like an album
- Read PDFs, text and code files (with syntax colouring), and rendered Markdown in the viewer; text files show their first lines as a thumbnail
- Details panel with size, date and location, including the total size of a folder
- Download single files, or whole folders as a ZIP
- Upload from any device: pick files or drag and drop, with progress bars; on a computer, whole folders too
- Search by file name across subfolders, and sort by name, date, size or type
- Light and dark theme, following the system or chosen by hand
- QR code at startup, so a phone can open it instantly
- Optional password protection
- A single executable with the web UI built in; no install, no database

## Download

Grab the build for your system from the [latest release](../../releases/latest):

| System | File |
| --- | --- |
| Mac (Apple Silicon) | `mediabrowser-mac-arm64` |
| Mac (Intel) | `mediabrowser-mac-intel` |
| Windows | `mediabrowser-windows.exe` |
| Linux (PC) | `mediabrowser-linux-amd64` |
| Linux (Raspberry Pi 64-bit, ARM) | `mediabrowser-linux-arm64` |

On Mac and Linux, make the download executable first. On a Mac, also clear the
"downloaded from the internet" flag, since the builds aren't signed by Apple:

```sh
chmod +x mediabrowser-mac-arm64
xattr -d com.apple.quarantine mediabrowser-mac-arm64   # Mac only
```

## Usage

```sh
mediabrowser ~/Pictures
mediabrowser --port 9000 --password secret ~/Pictures
```

It prints the addresses to open, plus a QR code for your phone.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--port` | `8080` | Port to listen on |
| `--password` | none | Require a password to browse or upload |
| `--version` | | Print the version and exit |

### Video thumbnails

Image thumbnails work out of the box. For **video** thumbnails (and HEIC/iPhone
photo thumbnails, and album art and waveforms for audio), install [ffmpeg](https://ffmpeg.org) and make sure it's on
your `PATH`:

```sh
brew install ffmpeg        # Mac
sudo apt install ffmpeg    # Debian, Ubuntu, Raspberry Pi OS
winget install ffmpeg      # Windows
```

Without ffmpeg, videos and audio show a plain icon. Thumbnails are cached in your user
cache folder (e.g. `~/Library/Caches/mediabrowser` on a Mac), not in your media
folder.

### Security

This is meant for a trusted home network. It uses plain HTTP and, without
`--password`, anyone on the network can view and upload files. Don't expose it
to the internet with port forwarding. For access away from home, use a private
network tool such as [Tailscale](https://tailscale.com).

Uploads never overwrite existing files (`photo.jpg` becomes `photo (1).jpg`),
and requests can't reach files outside the folder you shared. Hidden files
(names starting with `.`) aren't shown or served. Files that could run scripts,
such as HTML or SVG, open in a sandbox, so they can't use your login to the app.

## Building from source

Requires [Go](https://go.dev) 1.26 or newer.

```sh
make                         # build ./mediabrowser
make run DIR=~/Pictures      # build and run
make dist                    # build every platform into dist/
```

The web UI lives in `web/` and is embedded into the executable at build time,
so rebuild after changing it.

### Web libraries

The browser libraries the UI uses (marked and highlight.js) are pinned in
`package.json` and copied into `web/vendor/`, which is committed, so building
needs only Go. To update them you need [Node](https://nodejs.org):

```sh
npm install marked@latest   # or edit the version in package.json
make vendor                 # refresh web/vendor from package.json
```

Dependabot opens a weekly pull request when new versions come out. It only
updates `package.json` and the lockfile, so CI fails on it until you check out
its branch, run `make vendor` and push. The highlight.js languages to bundle are
listed in `web/syntax.js`.

### Releasing

Push a version tag and GitHub Actions builds every platform and publishes a release:

```sh
git tag v0.2.0
git push origin v0.2.0
```

## License

[GPL-3.0](LICENSE)

The web UI bundles [Inter](https://rsms.me/inter/) ([OFL](web/fonts/OFL.txt)),
icons from [Lucide](https://lucide.dev) (ISC),
[marked](https://marked.js.org) ([MIT](web/vendor/marked-LICENSE.txt)) for Markdown and
[highlight.js](https://highlightjs.org) ([BSD-3-Clause](web/vendor/highlight/LICENSE.txt)) for syntax colouring.
