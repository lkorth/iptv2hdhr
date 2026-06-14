package fetchutil

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGet_FetchesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	}))
	defer srv.Close()

	data, err := Get(context.Background(), http.DefaultClient, srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("data = %q, want %q", data, "hello")
	}
}

func TestGetConditional_ReturnsValidatorsAndData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
		w.Write([]byte("hello"))
	}))
	defer srv.Close()

	result, err := GetConditional(context.Background(), http.DefaultClient, srv.URL, "", "")
	if err != nil {
		t.Fatalf("GetConditional: %v", err)
	}
	if result.NotModified {
		t.Errorf("NotModified = true, want false")
	}
	if string(result.Data) != "hello" {
		t.Errorf("Data = %q, want %q", result.Data, "hello")
	}
	if result.ETag != `"v1"` {
		t.Errorf("ETag = %q, want %q", result.ETag, `"v1"`)
	}
	if result.LastModified != "Mon, 02 Jan 2006 15:04:05 GMT" {
		t.Errorf("LastModified = %q, want %q", result.LastModified, "Mon, 02 Jan 2006 15:04:05 GMT")
	}
}

func TestGetConditional_NotModified(t *testing.T) {
	const etag = `"v1"`
	const lastModified = "Mon, 02 Jan 2006 15:04:05 GMT"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != etag {
			t.Errorf("If-None-Match = %q, want %q", r.Header.Get("If-None-Match"), etag)
		}
		if r.Header.Get("If-Modified-Since") != lastModified {
			t.Errorf("If-Modified-Since = %q, want %q", r.Header.Get("If-Modified-Since"), lastModified)
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	result, err := GetConditional(context.Background(), http.DefaultClient, srv.URL, etag, lastModified)
	if err != nil {
		t.Fatalf("GetConditional: %v", err)
	}
	if !result.NotModified {
		t.Errorf("NotModified = false, want true")
	}
	if len(result.Data) != 0 {
		t.Errorf("Data = %q, want empty", result.Data)
	}
}
