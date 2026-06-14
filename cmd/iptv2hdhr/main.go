// Command iptv2hdhr runs the IPTV-to-HDHomeRun emulation service.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"iptv2hdhr/internal/config"
	"iptv2hdhr/internal/guide"
	"iptv2hdhr/internal/lineup"
	"iptv2hdhr/internal/playlist"
	"iptv2hdhr/internal/server"
	"iptv2hdhr/internal/ssdp"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	lin, err := lineup.New(cfg.Channels)
	if err != nil {
		log.Fatalf("building lineup: %v", err)
	}

	pf := playlist.NewFetcher(cfg.Playlists)
	gf := guide.NewFetcher(cfg.Guides)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initial synchronous fetch so the server doesn't start empty.
	pf.RefreshNow(ctx)
	lin.Rebuild(pf.Current())
	gf.RefreshNow(ctx)

	go pf.Start(ctx, lin.Rebuild)
	go gf.Start(ctx)

	if err := advertise(ctx, cfg); err != nil {
		log.Printf("ssdp: %v", err)
	}

	addr := fmt.Sprintf(":%d", cfg.Device.HTTPPort)
	httpServer := &http.Server{
		Addr:    addr,
		Handler: server.New(ctx, cfg, lin, gf),
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
		httpServer.Shutdown(context.Background())
	}()

	log.Printf("listening on %s", addr)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server error: %v", err)
	}
}

// advertise starts SSDP advertisement of this device, using the configured
// base URL if set, or the host's primary outbound IP otherwise.
func advertise(ctx context.Context, cfg *config.Config) error {
	base := cfg.Device.BaseURL
	if base == "" {
		ip, err := outboundIP()
		if err != nil {
			return fmt.Errorf("determining outbound IP: %w", err)
		}
		base = fmt.Sprintf("http://%s:%d", ip, cfg.Device.HTTPPort)
	}
	return ssdp.Advertise(ctx, cfg.Device.DeviceID, base+"/device.xml", "iptv2hdhr/1.0")
}

// outboundIP returns the local IP address used to reach the public
// internet, as a best-effort guess for SSDP advertisements when
// device.base_url isn't configured.
func outboundIP() (string, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", err
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String(), nil
}
