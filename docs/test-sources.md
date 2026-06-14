# Test IPTV/EPG Sources

For local development and testing, use the [iptv-org](https://github.com/iptv-org)
projects as sources of real-world M3U playlists and XMLTV guide data. They're
free, public, well-maintained, and exercise a much wider variety of edge cases
than the small fixtures in `testdata/`.

## iptv-org/iptv

https://github.com/iptv-org/iptv

A large collection of publicly available IPTV channels, organized as M3U
playlists. Useful for:

- Sanity-checking the M3U parser against real-world `#EXTINF` attribute
  variations (missing `tvg-id`, unusual `group-title` values, duplicate
  channel names, etc.) beyond what's covered in `testdata/sample.m3u`.
- Generating a large lineup (thousands of channels) to test `/lineup.json`
  performance and pagination/limits.
- Country- or category-specific playlists, e.g.:
  - All channels: `https://iptv-org.github.io/iptv/index.m3u`
  - By country (e.g. US): `https://iptv-org.github.io/iptv/countries/us.m3u`
  - By category (e.g. news): `https://iptv-org.github.io/iptv/categories/news.m3u`

Note: many streams in this list are unstable or geo-restricted, so it's best
suited for testing parsing/lineup logic, not for verifying actual stream
playback.

## iptv-org/epg

https://github.com/iptv-org/epg

Scripts and generated XMLTV guide data for channels across many providers.
Useful for:

- Testing the XMLTV guide generator against real `<programme>` data —
  multiple sources, overlapping/missing time ranges, varied `<channel id>`
  formats — beyond the single fixture in `testdata/sample-guide.xml`.
- Verifying the "real guide data vs. filler programme" logic: pick a
  `tvg-id` that exists in an iptv-org guide for the "real EPG" case, and one
  that doesn't for the "filler" case.
- Matching `tvg-id` values from `iptv-org/iptv` playlists against `<channel
  id>` values in the corresponding `iptv-org/epg` guide output, to test the
  channel/EPG mapping end-to-end.

Pre-generated guide files for some sites are published under this repo's
[releases](https://github.com/iptv-org/epg/releases) and `guides/` output —
check the repo for current download links, since generation is run on a
schedule and paths may change.

## Recommended use

- Keep the small, hand-crafted fixtures in `testdata/` for fast unit
  tests (no network access required).
- Use iptv-org data for integration/manual testing: fetch a playlist and a
  matching guide, point the tuner config at them, and confirm `/lineup.json`
  stays stable across reloads while the XMLTV guide content updates.
- Don't commit large iptv-org playlist/guide files into the repo — fetch them
  on demand (e.g. via `curl`) when needed for manual testing.
