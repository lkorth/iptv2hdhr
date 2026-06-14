# Development

## Building and testing

`./dev.sh` wraps `go` commands in the `golang:1.22` Docker image, mounting
the project at `/src` and mapping port 8080:

```sh
./dev.sh build ./...
./dev.sh test ./...
./dev.sh run ./cmd/iptv2hdhr -config testdata/sample-config.yaml
```

## Manual verification (no Plex required)

1. Start the server with the sample config (uses `file://` fixtures, no
   network needed):

   ```sh
   ./dev.sh run ./cmd/iptv2hdhr -config testdata/sample-config.yaml
   ```

2. `curl http://localhost:8080/discover.json` — `DeviceID` should match
   `device.device_id` from the config. Restart the server and re-check: it
   should be unchanged, since it now comes from the config file rather than
   being generated.

3. `curl http://localhost:8080/lineup.json` — should list channels 1001,
   1002, 1003, 1004, 2001 in that order, each with a `URL` of the form
   `http://<host>/stream/<number>`.

4. `curl -i http://localhost:8080/stream/1001` — matches the sample
   playlist's ESPN entry (`http://example.com/streams/espn`), so the proxy
   will attempt to fetch that upstream (will fail/503 unless that host is
   reachable — this confirms the matching + proxy wiring, not real
   playback).

5. `curl -i http://localhost:8080/stream/2001` — configured but matches
   nothing in the sample playlist -> expect **503**.

6. `curl -i http://localhost:8080/stream/9999` — not configured at all ->
   expect **404**.

7. `curl http://localhost:8080/guide.xml` — well-formed XMLTV. Channels
   `slot-1001`/`slot-1002` should have real programmes from
   `testdata/sample-guide.xml`; `slot-1003`/`slot-1004`/`slot-2001` should be
   entirely "No Program Information Available" filler.

8. `curl http://localhost:8080/device.xml` — well-formed UPnP device
   description; `UDN` should be `uuid:<DeviceID>` matching step 2.

9. Lineup stability: edit `testdata/sample.m3u` (e.g. remove the ESPN entry),
   wait ~1 minute for the playlist refresh, and re-curl `/lineup.json` — the
   channel list must be byte-identical to step 3, even though
   `/stream/1001` may now return 503.

### Using real-world test data

For more realistic testing (large playlists, real EPG data, edge cases not
covered by the small fixtures in `testdata/`), see
`docs/test-sources.md` — points at the `iptv-org/iptv` and
`iptv-org/epg` projects for free public M3U/XMLTV sources.
