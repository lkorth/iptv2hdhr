# Plex DVR / Live TV Integration Notes

How Plex's "Live TV & DVR" feature interacts with an HDHomeRun-emulating backend.

## Setup flow

1. User adds a DVR in Plex settings → Plex auto-discovers tuners via SSDP, or
   the user enters the tuner's address manually.
2. Plex fetches `/discover.json` to get `DeviceID`, `LineupURL`, etc.
3. Plex fetches `/lineup.json` and runs its "channel scan" — this is a
   **one-time snapshot**. Plex stores this channel list internally
   (number, name → maps to a Plex "Media Provider" channel entry).
4. User picks an XMLTV guide source (or uses Plex's built-in guide data,
   matched by postal code/lineup — not useful for custom IPTV channels).
   **Plex only supports one EPG source for all tuners**, and once XMLTV is
   configured, Plex fetches `guide.xml` from the URL you gave it on its own
   refresh schedule. You cannot use Plex's zipcode based EPG along with a
   custom XMLTV, you will need to use XMLTV for all guide data and add guide
   data for all your tuners to it, including those outside of this service.
   If you wish to utilize Plex's EPG source one work around is to run
   multiple Plex instances.
5. Plex performs "channel mapping" — matching each lineup.json entry to a
   `<channel>` in the XMLTV by `tvg-id`/`channel id` attributes (Threadfin's
   XEPG layer exists specifically to control this mapping).

## What causes "needs a rescan"

- **Lineup count/identity changes**: if `/lineup.json` returns a different
  set of `GuideNumber`s than what Plex scanned originally (new channel added,
  old one removed, or a `GuideNumber` reused for a different channel), Plex's
  internal channel records go stale. Plex has no "lineup changed, please
  refresh" push mechanism for HDHomeRun-style tuners — the user has to go into
  Plex's tuner settings and hit "Scan for Channels" again, which re-runs step
  3 above.
- **Channel mapping breaking**: if the XMLTV `<channel id>` that a Plex
  channel was mapped to disappears from the guide feed, Plex shows "no guide
  data" for that channel and may need re-mapping (a manual step in Plex UI),
  separate from a lineup rescan.

## What does NOT cause a rescan

- **XMLTV guide content changes** (programs added/updated/removed) — Plex
  refreshes `guide.xml` on its own schedule (and the user can hit "Refresh
  guide" in Plex settings) without touching the lineup at all.
- **Stream URL changes** — `lineup.json`'s `URL` field can change between
  scans without issue, as long as `GuideNumber`/`GuideName` stay put. Plex
  re-resolves the URL each time it tunes a channel (it doesn't cache the URL
  from the scan).

## Design implication: the "stable lineup" contract

To avoid the need for manual rescans by the user:

1. **`DeviceID` is a user-set config value (`device.device_id`)**, chosen
   once and never changed.
2. **Channel list is config, not derived.** Define a fixed set of channel
   slots (number + name + logo + "what M3U entries can fill this slot") that
   the user manages explicitly. `/lineup.json` always returns exactly this
   set, in the same order, with the same `GuideNumber`s — regardless of
   whether the upstream M3U currently has a matching stream.
3. **XMLTV guide is regenerated freely and often** (e.g. every few minutes),
   independent of the lineup:
   - If real program data exists for a channel/timeslot (from an XMLTV EPG
     source matched via `tvg-id`), use it.
   - If not, synthesize a filler `<programme>` covering the gap — e.g.
     "No Program Information Available" / channel name as title — so Plex's
     guide grid is never empty for a channel. Filler programs should be
     reasonably short-lived (e.g. 1-4 hours) and regenerated each refresh so
     Plex always sees *something* scheduled "now" and "next", and real
     program data can slot in seamlessly once it appears.
   - This means a channel can go from "filler" to "real show" to "filler"
     across guide refreshes without ever touching `/lineup.json` — no rescan
     needed.
4. **Adding/removing a channel is a deliberate config change** (and the user
   accepts that *that* will need a Plex rescan — but it's a manual, bounded
   action vs. happening any time an IPTV provider's playlist shuffles).

## Practical recommendations

- Generate XMLTV with a rolling window (e.g. now − 1h to now + 24-48h) on
  every refresh.
- Keep `<channel id>` stable per channel slot (e.g. `slot-1001`) and have
  `/lineup.json`'s implicit mapping + the XMLTV `<channel id>` always agree —
  avoids relying on Plex's manual channel-mapping UI at all if `tvg-id`s in
  lineup responses and XMLTV match 1:1 from the start.
- Plex's guide refresh interval is not user-configurable below a certain
  floor; serving a `guide.xml` that's cheap to (re)generate on every request
  is simplest — no need for complex caching.

## Sources
- Plex Support, "Live TV & DVR (Set Up and Manage)", https://support.plex.tv/articles/225877347-live-tv-dvr/
- Plex Support, "Using an XMLTV guide", https://support.plex.tv/articles/using-an-xmltv-guide/
- Community/Threadfin behavior observed in `Threadfin/src/xepg.go`, `Threadfin/src/hdhr.go`
