package kademlia

import (
	"context"
	"fmt"
	"time"

	"github.com/sanskarpan/dht-system/dht-system/internal/consistent"
	"github.com/sanskarpan/dht-system/dht-system/internal/events"
	"github.com/sanskarpan/dht-system/dht-system/internal/transport"
)

// Join bootstraps this node into the Kademlia network via a known contact.
func (n *KademliaNode) Join(ctx context.Context, bootstrap Contact) error {
	// Step 1: Add bootstrap to routing table
	n.updateRoutingTable(bootstrap)

	// Step 2: ITERATIVE_FIND_NODE(self.ID) — self-lookup populates routing table
	_, _, err := n.IterativeFindNode(ctx, n.ID)
	if err != nil {
		return fmt.Errorf("kademlia.Join self-lookup: %w", err)
	}

	// Step 3: Bucket refresh — lookup a random ID in each empty bucket
	go n.refreshBuckets()

	n.Bus.Publish(events.MakeEvent(events.EventNodeJoin, events.NodeJoinPayload{
		NodeID: consistent.IDToHex([20]byte(n.ID)),
		Addr:   n.Addr,
	}))
	return nil
}

// CreateNetwork initializes this node as the first node in a new Kademlia network.
func (n *KademliaNode) CreateNetwork() {
	n.Bus.Publish(events.MakeEvent(events.EventNodeJoin, events.NodeJoinPayload{
		NodeID: consistent.IDToHex([20]byte(n.ID)),
		Addr:   n.Addr,
	}))
}

// ensure time is used
var _ = time.Now

// ensure transport is used
var _ = transport.NodeRef{}

// ensure fmt is used
var _ = fmt.Sprintf
