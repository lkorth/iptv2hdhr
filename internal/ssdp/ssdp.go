// Package ssdp advertises this server as a UPnP root device so that Plex
// and other HDHomeRun clients can discover it automatically on the local
// network.
package ssdp

import (
	"context"
	"fmt"
	"time"

	"github.com/koron/go-ssdp"
)

// alivePeriod is how often we re-announce presence on the network.
const alivePeriod = 300 * time.Second

// maxAge is the SSDP advertisement's max-age, in seconds.
const maxAge = 1800

// Advertise starts SSDP advertisement of this device as a UPnP root device.
// location should be the absolute URL of /device.xml (e.g.
// "http://192.168.1.50:8080/device.xml"). The advertisement runs until ctx
// is done, at which point it sends a "byebye" notification and stops.
func Advertise(ctx context.Context, deviceID, location, server string) error {
	ad, err := ssdp.Advertise(
		"upnp:rootdevice",
		fmt.Sprintf("uuid:%s::upnp:rootdevice", deviceID),
		location,
		server,
		maxAge,
	)
	if err != nil {
		return fmt.Errorf("starting SSDP advertisement: %w", err)
	}

	go func() {
		ticker := time.NewTicker(alivePeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := ad.Alive(); err != nil {
					return
				}
			case <-ctx.Done():
				ad.Bye()
				ad.Close()
				return
			}
		}
	}()

	return nil
}
