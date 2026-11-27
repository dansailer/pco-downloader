# Security

`pco-dl` talks only to the Planning Center API and to file URLs that API returns. It does not scrape the site, reuse a browser cookie, or open a player URL of its own.

## Token

The personal access token is read from `PCO_APP_ID` and `PCO_SECRET`, or from one config file. That file is mode `0600` in a directory mode `0700`. A symlink at the file or directory is refused. A file that other users can read is tightened to `0600` on the next load. Values are limited to 512 bytes and cannot contain newlines.

The token is sent only to the configured API origin, and only on `/services/` paths. The scheme and port have to match that origin. A link that carries its own username or password is refused. Cleartext is allowed only when the API host is local. Download hosts do not receive the token. ffmpeg does not receive `PCO_APP_ID`, `PCO_SECRET`, or the proxy variables. Errors omit query strings and raw response bodies.

## Downloads

File URLs must be `https`, unless the configured API host is itself local. Redirects stop after five. Private, loopback, link-local, metadata, and carrier-grade NAT addresses are refused. A public API name that resolves to one of those addresses is refused. The connection uses the address that passed that check.

A proxy set in `HTTPS_PROXY`, `HTTP_PROXY`, or `ALL_PROXY` is dialed as configured. Other connections are not.

Each download stops at 512 MiB. The output name is one file in the destination directory: the song title, `_`, and the file name, with path separators removed. An existing symlink is not followed. ffmpeg reads a temp file only, with protocols limited to `file`, `pipe`, and `crypto`. The file it writes is an absolute path, so a name that starts with `-` is not another ffmpeg option.

## Releases

Workflows pin every action to a commit SHA. CI can read the repository. Release builds can upload artifacts, and only the publish job can create a release. A tag that is not already on the remote is refused, and a tag that is not `vMAJOR.MINOR.PATCH` fails the build. Binaries are built with `CGO_ENABLED=0`, `-trimpath`, and `-ldflags "-s -w"`. Compare `SHA256SUMS` with the asset you download.

Pinned actions are checked by `jactionlint update`. A newer release is reported only after it is seven days old. ffmpeg is not part of the release. Install it from your package manager when a stream is not already MP3.

There is no license file yet. The repository is public. Do not treat the source as reusable until a license is added.

Report a vulnerability through a [private GitHub security advisory](https://github.com/dansailer/pco-downloader/security/advisories/new).
