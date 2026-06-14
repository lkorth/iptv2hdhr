# IPTV → Plex HDHomeRun

## Goal
Build a self-hosted service that:
- Lets the user configure one or more M3U playlists (IPTV sources).
- Emulates an HDHomeRun tuner so Plex can use it as a Live TV/DVR source
  (same approach Threadfin/xTeVe use).
- Presents a **stable channel lineup to Plex** — the same number of channels,
  same channel numbers/names, every time `/lineup.json` is requested — so
  Plex never needs a manual "Scan for Channels" after the initial setup.
- Generates an XMLTV guide that shows real "what's on now/next" when EPG data
  is available for a channel, and a "nothing scheduled" filler programme
  otherwise. Guide content can change freely on every refresh without
  affecting the lineup.

## Reference material
- `docs/hdhomerun-protocol.md` — HDHomeRun HTTP emulation spec (discover.json,
  lineup.json, lineup_status.json, device.xml, SSDP) condensed from
  SiliconDust's official docs + Threadfin's implementation.
- `docs/plex-dvr-integration.md` — how Plex's DVR setup/scan/guide-refresh
  flow works, and specifically what does/doesn't trigger a rescan. This is
  the basis for the "stable lineup, dynamic guide" design.
- `docs/test-sources.md` — recommended IPTV/EPG sources for testing
  ([iptv-org/iptv](https://github.com/iptv-org/iptv) and
  [iptv-org/epg](https://github.com/iptv-org/epg)).

## Development environment
- **Always run `go`/build/test commands via
  `./dev.sh`** (e.g. `./dev.sh test ./...`, `./dev.sh build ./...`,
  `./dev.sh run ./cmd/iptv2hdhr -config ...`), which runs them
  inside the `golang:1.22` Docker image. Don't try `go` directly.
- See `DEVELOPMENT.md` for the full build/test workflow and the manual
  verification steps (no Plex required).
