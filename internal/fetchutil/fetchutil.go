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

// Result is the outcome of a conditional fetch.
type Result struct {
	// Data is the (gunzipped) response body. Empty when NotModified is true.
	Data []byte
	// NotModified is true if the server confirmed the previously-fetched
	// content (identified by the etag/lastModified validators passed to
	// GetConditional) is still current.
	NotModified bool
	// ETag and LastModified are the validators returned with this response,
	// to pass back to GetConditional on the next refresh. Only set when the
	// server provides them.
	ETag         string
	LastModified string
}

// Get fetches the contents at rawURL. It supports http:// and https:// (via
// client), and file:// for local testing/development. The result is passed
// through MaybeGunzip, since many IPTV/EPG providers serve gzip-compressed
// content without setting Content-Encoding.
func Get(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	result, err := GetConditional(ctx, client, rawURL, "", "")
	if err != nil {
		return nil, err
	}
	return result.Data, nil
}

// GetConditional fetches rawURL like Get, but for http(s):// URLs sends
// If-None-Match/If-Modified-Since based on the etag/lastModified validators
// from a previous fetch (pass "" for either if unknown). If the server
// responds 304 Not Modified, Result.NotModified is true and Data is empty,
// letting callers skip re-parsing unchanged playlists/guides.
//
// file:// URLs ignore the validators and always return the full content,
// since conditional requests don't apply to local files.
func GetConditional(ctx context.Context, client *http.Client, rawURL, etag, lastModified string) (Result, error) {
	if path, ok := fileURLPath(rawURL); ok {
		data, err := os.ReadFile(path)
		if err != nil {
			return Result{}, fmt.Errorf("reading %s: %w", rawURL, err)
		}
		data, err = MaybeGunzip(data)
		if err != nil {
			return Result{}, err
		}
		return Result{Data: data}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Result{}, fmt.Errorf("building request for %s: %w", rawURL, err)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}

	resp, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("fetching %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return Result{
			NotModified:  true,
			ETag:         resp.Header.Get("ETag"),
			LastModified: resp.Header.Get("Last-Modified"),
		}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("fetching %s: unexpected status %d", rawURL, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{}, fmt.Errorf("reading response body from %s: %w", rawURL, err)
	}

	data, err = MaybeGunzip(data)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Data:         data,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}, nil
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
