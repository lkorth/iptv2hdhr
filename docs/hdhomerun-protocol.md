# HDHomeRun HTTP Emulation Protocol (for Plex compatibility)

This is the practical subset of the HDHomeRun protocol that a software "tuner"
needs to implement so Plex (and other clients: Jellyfin, Emby, Channels) will
detect it, treat it as a real HDHomeRun device, and use it as a Live TV/DVR
source. Compiled from the official SiliconDust "HDHomeRun HTTP Development
Guide (20140407)" and cross-referenced against Threadfin's implementation
(`Threadfin/src/hdhr.go`, `struct-hdhr.go`, `ssdp.go`).

## 1. Discovery

### SSDP/UPnP (network discovery)
- The emulator advertises itself via SSDP (`upnp:rootdevice`), pointing at
  `http://<host>:<port>/device.xml`.
- Plex's "search the network" step in the DVR setup wizard relies on this.
- Threadfin uses `github.com/koron/go-ssdp` to send periodic `Alive`
  notifications (every 300s) — see `Threadfin/src/ssdp.go`.
- This can be skipped if the tuner is added manually by IP/hostname in Plex,
  but auto-discovery is what makes setup "just work".

### HDHomeRun native discovery (UDP 65001)
- Real HDHomeRun devices also respond to a binary TLV protocol broadcast on
  UDP port 65001. Plex does **not** require this for HTTP-based tuners — the
  SSDP + HTTP JSON API below is sufficient.

## 2. Required HTTP Endpoints

All of these are plain HTTP (no auth) on whatever port the emulator listens on.

### `GET /discover.json`
Device identity. **`DeviceID` is the most important field** — Plex uses it as
the stable key for "this DVR". If `DeviceID` changes, Plex treats it as a
*different* device and you'll get duplicate DVR entries in Plex's UI.

```json
{
  "FriendlyName": "My IPTV Tuner",
  "ModelNumber": "HDTC-2US",
  "FirmwareName": "bin_1.0",
  "FirmwareVersion": "1.0",
  "DeviceID": "12345678",
  "DeviceAuth": "...",
  "BaseURL": "http://192.168.1.50:8080",
  "LineupURL": "http://192.168.1.50:8080/lineup.json",
  "TunerCount": 4
}
```

- `DeviceID` must be **persisted** (generated once, stored on disk, never
  regenerated) — this is the anchor of Plex's tuner identity.
- `TunerCount` limits how many simultaneous streams Plex will pull. Set this
  to however many concurrent streams your upstream IPTV connection(s) support.

### `GET /lineup_status.json`
Scan state. Plex polls this during/after a channel scan.

```json
{
  "ScanInProgress": 0,
  "ScanPossible": 1,
  "Source": "Cable",
  "SourceList": ["Cable"]
}
```

### `GET /lineup.json` (and optionally `/lineup.xml`)
**The channel list.** This is the core of the "stable lineup" requirement.
Per spec, each entry is:

```json
[
  {
    "GuideNumber": "1001",
    "GuideName": "Example Channel",
    "URL": "http://192.168.1.50:8080/stream/1001",
    "Tags": "favorite"
  }
]
```

- `GuideNumber`: virtual channel number. ATSC tuners use `n.n` format;
  for IPTV, plain integers (as strings) are fine and is what Threadfin does.
- `GuideName`: UTF-8 channel name.
- `URL`: where Plex pulls the MPEG-TS stream from. Does **not** have to be the
  raw upstream IPTV URL — should point back at the emulator so it can proxy,
  rewrite, handle reconnects, etc.
- `Tags`: optional CSV (`favorite`, `drm`).

Plex's "Scan for Channels" step calls this endpoint once and stores the result.
As long as this list's `GuideNumber`/`GuideName`/count stays the same across
calls, Plex has no reason to think anything changed — there is nothing to
"rescan". The actual program data (what's *on* that channel right now) comes
from a **separate** XMLTV guide fetch, which Plex refreshes periodically
without touching the lineup at all. See `plex-dvr-integration.md`.

### `GET /lineup.post?scan=start` / `?scan=abort`
Triggers/aborts a channel scan. Since our lineup is static/config-driven, this
can be a no-op that immediately reports `ScanInProgress: 0`.

### `GET /device.xml` (UPnP description, referenced by SSDP `LOCATION`)
```xml
<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
  <specVersion><major>1</major><minor>0</minor></specVersion>
  <URLBase>http://192.168.1.50:8080</URLBase>
  <device>
    <deviceType>urn:schemas-upnp-org:device:MediaServer:1</deviceType>
    <friendlyName>My IPTV Tuner</friendlyName>
    <manufacturer>Silicondust</manufacturer>
    <modelName>HDTC-2US</modelName>
    <modelNumber>HDTC-2US</modelNumber>
    <UDN>uuid:DEVICE-ID-HERE</UDN>
  </device>
</root>
```

## 3. Streaming

`GET <stream URL from lineup.json>` must return a live MPEG-TS stream over
HTTP and keep streaming until the client disconnects. Notes from the spec
that matter for an implementation:

- No fixed content-length — stream until TCP close.
- `?duration=<seconds>` is an optional param real HDHomeRuns support to
  auto-close the stream; not required for us but cheap to support.
- If the channel doesn't exist, return `404`. If no tuner/stream is available,
  return `503` (Plex will treat this as "channel temporarily unavailable"
  rather than "channel doesn't exist" — important for not triggering rescans
  when an upstream IPTV source is briefly down).
- Buffering: clients expect time-based buffering of a real-time stream. If we
  proxy from an upstream IPTV URL, we mostly just need to pipe bytes through
  without re-muxing (unless transcoding is needed for compatibility).

## 4. Field/behavior summary table

| Endpoint | Purpose | Must be stable? |
|---|---|---|
| `/discover.json` `DeviceID` | Plex's identity key for this DVR | Yes — persist forever |
| `/lineup.json` | Channel list (number, name, url) | Yes — this is the "no rescan" contract |
| `/lineup_status.json` | Scan progress | No (just report idle) |
| `/device.xml` + SSDP | Auto-discovery | `UDN` should match `DeviceID` |
| XMLTV guide (separate fetch) | "What's on now" | No — refresh freely |

## Sources
- SiliconDust, "HDHomeRun HTTP Development Guide (20140407)",
  https://www.silicondust.com/hdhomerun/hdhomerun_http_development.pdf
- Threadfin source: `Threadfin/src/hdhr.go`, `struct-hdhr.go`, `ssdp.go`
