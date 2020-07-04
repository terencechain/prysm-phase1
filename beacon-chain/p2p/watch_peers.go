package p2p

import (
	"context"
	"time"

	host "github.com/libp2p/go-libp2p-host"
)

const timeoutMax = 30 * time.Second

// ensurePeerConnections will attempt to reestablish connection to the peers
// if there are currently no connections to that peer.
func ensurePeerConnections(ctx context.Context, h host.Host, peers ...string) {
	if len(peers) == 0 {
		return
	}
	for _, p := range peers {
		if p == "" {
			continue
		}
		peer, err := MakePeer(p)
		if err != nil {
			log.Errorf("Could not make peer: %v", err)
			continue
		}

		c := h.Network().ConnsToPeer(peer.ID)
		if len(c) == 0 {
			log.WithField("peer", peer.ID).Debug("No connections to peer, reconnecting")
			ctx, cancel := context.WithTimeout(ctx, timeoutMax)
			defer cancel()
			if err := h.Connect(ctx, *peer); err != nil {
				log.WithField("peer", peer.ID).WithField("addrs", peer.Addrs).WithError(err).Errorf("Failed to reconnect to peer")
				continue
			}
		}
	}
}
