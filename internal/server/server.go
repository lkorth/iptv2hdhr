// Package server wires together the HDHomeRun emulation, guide, and stream
// proxy handlers into a single HTTP handler.
package server

import (
	"compress/gzip"
	"context"
	"net/http"
	"strings"
	"time"

	"iptv2hdhr/internal/config"
	"iptv2hdhr/internal/guide"
	"iptv2hdhr/internal/hdhr"
	"iptv2hdhr/internal/lineup"
	"iptv2hdhr/internal/streamproxy"
)

// Server is the top-level HTTP handler for the tuner.
type Server struct {
	mux *http.ServeMux
}

// New builds a Server with all routes registered. ctx bounds the lifetime of
// shared stream sessions (upstream connections and ffmpeg processes): when
// it's cancelled, e.g. on server shutdown, they're all torn down.
func New(ctx context.Context, cfg *config.Config, lin *lineup.Lineup, gf *guide.Fetcher) *Server {
	mux := http.NewServeMux()

	deps := &hdhr.Deps{Cfg: cfg, Lineup: lin}
	mux.HandleFunc("/discover.json", deps.DiscoverHandler)
	mux.HandleFunc("/lineup_status.json", deps.LineupStatusHandler)
	mux.HandleFunc("/lineup.json", deps.LineupHandler)
	mux.HandleFunc("/lineup.post", deps.LineupPostHandler)
	mux.HandleFunc("/device.xml", deps.DeviceXMLHandler)

	mux.HandleFunc("/guide.xml", guideHandler(lin, gf))

	mux.Handle("/stream/", streamproxy.New(lin, ctx))

	return &Server{mux: mux}
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// guideHandler serves /guide.xml, generated fresh on every request. XMLTV
// guides can run into the tens of megabytes for a full lineup, so the
// response is gzip-compressed when the client (Plex) advertises support for
// it.
func guideHandler(lin *lineup.Lineup, gf *guide.Fetcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := guide.BuildGuideXML(lin.Current(), gf.Current(), time.Now())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(w)
			gz.Write(data)
			gz.Close()
			return
		}
		w.Write(data)
	}
}
