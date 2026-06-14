// Package fetchutil provides small shared helpers for fetching playlist and
// guide data over HTTP(S) or from local files.
package fetchutil

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// Get fetches the contents at rawURL. It supports http:// and https:// (via
// client), and file:// for local testing/development. The result is passed
// through MaybeGunzip, since many IPTV/EPG providers serve gzip-compressed
// content without setting Content-Encoding.
func Get(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	var data []byte

	if path, ok := fileURLPath(rawURL); ok {
		var err error
		data, err = os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", rawURL, err)
		}
	} else {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, fmt.Errorf("building request for %s: %w", rawURL, err)
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetching %s: %w", rawURL, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetching %s: unexpected status %d", rawURL, resp.StatusCode)
		}

		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("reading response body from %s: %w", rawURL, err)
		}
	}

	return MaybeGunzip(data)
}

// fileURLPath returns the filesystem path for a file:// URL, or ok=false if
// rawURL doesn't use the file scheme.
func fileURLPath(rawURL string) (string, bool) {
	const prefix = "file://"
	if !strings.HasPrefix(rawURL, prefix) {
		return "", false
	}
	return strings.TrimPrefix(rawURL, prefix), true
}

// MaybeGunzip transparently decompresses data if it looks gzip-compressed
// (gzip magic bytes 0x1f 0x8b), regardless of what Content-Encoding (if any)
// was reported.
func MaybeGunzip(data []byte) ([]byte, error) {
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		return data, nil
	}

	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decompressing gzip content: %w", err)
	}
	defer r.Close()

	out, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("decompressing gzip content: %w", err)
	}
	return out, nil
}
