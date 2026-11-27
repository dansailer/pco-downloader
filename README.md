# pco-dl

Download Planning Center Services song attachments from the command line. Practice audio that the song page only streams is saved as MP3.

Files land in your Downloads folder, named `<song>_<filename>`. An example is `Here I Am (ICF SN)_05_Here I Am BASS rehearsal.mp3`.

## Quickstart

Create a personal access token at <https://api.planningcenteronline.com/oauth/applications>. The account needs access to the song.

```bash
export PCO_APP_ID=your-application-id
export PCO_SECRET=your-secret
pco-dl 31193598
```

A song URL works too. An arrangement URL lists every arrangement of that song.

```text
#  Access      Arrangement  Key  Name
1  download    Default      —    Chart.pdf
2  download    Default      G    Bass rehearsal.mp3
3  stream-only Default      G    Guide.m4a
```

Then type a selection: `1,3-5`, `all`, or Enter to cancel.

Links such as Spotify are printed. They are not saved. PDFs and other non-audio files keep their original format.

## Install

Go is read from the `toolchain` line in `go.mod` via [mise](https://mise.jdx.dev).

```bash
mise install
mise run build
./bin/pco-dl -h
```

Release tags `vMAJOR.MINOR.PATCH` publish darwin, linux, and windows binaries (amd64 and arm64) plus `SHA256SUMS`. Check the checksum before you run a downloaded binary.

An MP3 is copied as-is. AAC, M4A, and HLS playlists need ffmpeg on `PATH`, with `libmp3lame`. Install it yourself:

```bash
# macOS
brew install ffmpeg

# Debian, Ubuntu
sudo apt install ffmpeg

# Windows
winget install Gyan.FFmpeg
```

## Credentials

| Source | When |
| --- | --- |
| `PCO_APP_ID` and `PCO_SECRET` | Used when both are set. Set both, or neither. |
| Config file | Mode `0600` in your config directory: `~/Library/Application Support/pco-downloader/credentials` on macOS, `~/.config/pco-downloader/credentials` on Linux. The command offers to save the token here. |
| Prompt | Used in a terminal when neither of the above is set. The secret is not echoed. |

`-o` saves into a different directory. That directory is created if needed. The default Downloads folder is not created for you.

## Development

```bash
mise install
mise run test
mise run lint
mise run build
```

`mise run updates` checks the pinned GitHub Actions. A release has to be seven days old before that check asks for it. Dependabot runs daily and opens one grouped pull request per ecosystem once that wait is over.

## Release

```bash
git tag v0.1.0
git push origin v0.1.0
```

The tag must be `vMAJOR.MINOR.PATCH`. The release workflow builds darwin, linux, and windows for amd64 and arm64 with `CGO_ENABLED=0`, writes `SHA256SUMS`, and publishes those files. It does not create a tag that is not already on the repository.
