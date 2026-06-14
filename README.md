# iptv2hdhr

A self-hosted service that turns one or more M3U/IPTV playlists into a
virtual HDHomeRun tuner that Plex (or any other HDHomeRun client) can use
for Live TV.

The channel list (count, numbers, names, order) comes entirely from
`config.yaml`'s `channels:` section and never changes on its own so Plex
never needs a rescan after the initial setup. Only the *content* behind
each channel (which upstream stream it proxies to, and what's in the EPG)
changes as playlists/guides refresh.

## Configuration

Copy `config.example.yaml` to `config.yaml` and edit:

- `device`: HDHomeRun identity (friendly name, model, tuner count, port,
  `device_id`, optional `base_url`).
- `playlists`: one or more M3U sources, merged together.
- `guides`: zero or more XMLTV sources, merged together. Channels with no
  matching guide data show a "No Program Information Available" filler.
- `channels`: the stable lineup. Each entry needs a unique `number` and a
  `name`, plus `tvg_id` and/or `name_match` to match it against playlist
  entries. A channel with neither always returns 503 (reserved slot).

`playlists[].url` / `guides[].url` accept `http://`, `https://`, or
`file://` (for local testing). Gzip-compressed content is handled
transparently regardless of `Content-Encoding`.

`device.device_id` is Plex's stable identity for this tuner (HDHomeRun
"DeviceID"/UDN). Set it once and never change it once Plex has discovered
the tuner, or Plex will treat it as a brand new device.

`/stream/{number}` automatically remuxes HLS upstreams (`.m3u8`) to MPEG-TS
via `ffmpeg` (bundled in the Docker image), since HDHomeRun clients expect a
continuous MPEG-TS stream rather than an HLS manifest. Upstreams that are
already MPEG-TS are passed through directly with no ffmpeg overhead. For HLS
upstreams, video is passed through unchanged but audio is re-encoded to AC-3
(at real-time pace) — Plex's HDHomeRun tuner emulation expects ATSC-style
AC-3 audio and fails to tune ("Could not tune channel") with AAC, even if
copied byte-for-byte.

## Running with real Plex

1. Build and run the production image:

   ```sh
   docker build -t iptv2hdhr .
   docker run -d --name iptv2hdhr --network host \
     -v /path/to/data:/data \
     iptv2hdhr
   ```

   `/path/to/data` must contain `config.yaml`. `--network host` is
   recommended so SSDP (UDP multicast) auto-discovery works; without it, add
   the tuner to Plex manually by IP:port.

2. In Plex: **Settings → Live TV & DVR → Set up Plex DVR**. It should
   auto-discover the tuner via SSDP (by `device.friendly_name`), or add
   manually via `http://<host>:<port>/discover.json`.

3. Run the channel scan once — this is the only time `/lineup.json` is
   snapshotted by Plex.

4. Add the guide: after the channel scan, Plex's setup wizard shows a "Guide"
   step that defaults to Plex's zip-code-based guide data. Look for a link
   such as "Use an XMLTV guide on the network" (or similar wording — it's a
   small text link, not a button) and enter
   `http://<host>:<port>/guide.xml` there. Note: if this option is not
   present it may be hidden because you are using Plex sourced EPGs, you need
   to remove all other tuners and re-add them using an XML EPG in order to use
   this option. Plex only supports one XMLTV source for all DVR tuners.

5. Confirm the guide grid populates (real programmes where available,
   filler elsewhere).

6. By default Plex only refreshes the XMLTV guide once every 24 hours. If
   your guide sources refresh more often than that (per `guides[].refresh_interval`
   in `config.yaml`), edit the DVR's settings in Plex (the gear/edit icon next
   to the tuner under **Settings → Live TV & DVR**) and lower "Refresh guide
   every" to match, so Plex picks up new programme data sooner.

7. To verify the stability guarantee: change `playlists[].url` to point at a
   different/reshuffled upstream M3U (or wait for the provider to update
   theirs), restart, and confirm Plex's channel list and numbers are
   unchanged — channels whose matches disappeared simply show as
   unavailable rather than vanishing.

## Development

See [DEVELOPMENT.md](DEVELOPMENT.md) for building, testing, and manually
verifying changes without a Plex server.

## License

[MIT](LICENSE)
