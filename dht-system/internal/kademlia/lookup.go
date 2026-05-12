package kademlia

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sanskarpan/dht-system/dht-system/internal/consistent"
	"github.com/sanskarpan/dht-system/dht-system/internal/events"
	"github.com/sanskarpan/dht-system/dht-system/internal/store"
	"github.com/sanskarpan/dht-system/dht-system/internal/transport"
)

type iterativeRoundResult struct {
	contact  Contact
	contacts []Contact
	entry    *store.ValueEntry
	err      error
}

// IterativeFindNode performs the Kademlia iterative node lookup.
// Returns up to k closest contacts to target, with a hop trace.
func (n *KademliaNode) IterativeFindNode(ctx context.Context, target NodeID) ([]Contact, []transport.HopEvent, error) {
	seeds := n.RoutingTable.KClosest(target, n.Config.K)

	roundFn := func(alphaCtx context.Context, batch []Contact, hopIdx int, hops *[]transport.HopEvent) ([]Contact, *store.ValueEntry) {
		results := n.runIterativeRound(batch, target, "kad_batch", hopIdx, hops, func(contact Contact) iterativeRoundResult {
			ref := transport.NodeRef{ID: [20]byte(contact.ID), Addr: contact.Addr}
			contacts, err := n.Transport.FindNode(alphaCtx, n.Ref(), ref, [20]byte(target))
			if err != nil {
				return iterativeRoundResult{contact: contact, err: err}
			}

			return iterativeRoundResult{
				contact:  contact,
				contacts: nodeRefsToContacts(contacts),
			}
		})
		var newContacts []Contact
		for r := range results {
			if r.err == nil {
				newContacts = append(newContacts, r.contacts...)
			}
		}
		return newContacts, nil
	}

	_, Pn, hops, err := n.iterativeSearch(ctx, target, seeds, roundFn)
	return Pn, hops, err
}

// IterativeFindValue performs FIND_VALUE: stops early if value is found.
// When a value is found on a remote node, it is cached on the closest node
// that was visited but did not have the value (per Kademlia §2.3).
func (n *KademliaNode) IterativeFindValue(ctx context.Context, key NodeID) (*store.ValueEntry, []Contact, []transport.HopEvent, error) {
	// Check local store first.
	if entry, ok := n.KVStore.Get([20]byte(key)); ok {
		return entry, nil, nil, nil
	}

	seeds := n.RoutingTable.KClosest(key, n.Config.K)
	if len(seeds) == 0 {
		return nil, nil, nil, nil
	}

	// closestMiss tracks the nearest node that responded without the value,
	// so we can cache the entry there once found (Kademlia §2.3).
	var closestMiss Contact
	var closestMissSet bool

	roundFn := func(alphaCtx context.Context, batch []Contact, hopIdx int, hops *[]transport.HopEvent) ([]Contact, *store.ValueEntry) {
		results := n.runIterativeRound(batch, key, "find_value", hopIdx, hops, func(contact Contact) iterativeRoundResult {
			ref := transport.NodeRef{ID: [20]byte(contact.ID), Addr: contact.Addr}
			res, err := n.Transport.FindValue(alphaCtx, n.Ref(), ref, [20]byte(key))
			if err != nil {
				return iterativeRoundResult{contact: contact, err: err}
			}
			if res.Found {
				return iterativeRoundResult{
					contact: contact,
					entry:   res.Entry,
				}
			}
			return iterativeRoundResult{
				contact:  contact,
				contacts: nodeRefsToContacts(res.Contacts),
			}
		})
		var newContacts []Contact
		for r := range results {
			if r.err != nil {
				continue
			}
			if r.entry != nil {
				// Cache on the closest node that missed (use outer ctx, not alphaCtx).
				if closestMissSet {
					cacheRef := transport.NodeRef{ID: [20]byte(closestMiss.ID), Addr: closestMiss.Addr}
					_ = n.Transport.KStore(ctx, n.Ref(), cacheRef, r.entry)
				}
				return nil, r.entry
			}
			// Track the closest miss for caching.
			if !closestMissSet || XORDistance(r.contact.ID, key).Cmp(XORDistance(closestMiss.ID, key)) < 0 {
				closestMiss = r.contact
				closestMissSet = true
			}
			newContacts = append(newContacts, r.contacts...)
		}
		return newContacts, nil
	}

	entry, Pn, hops, err := n.iterativeSearch(ctx, key, seeds, roundFn)
	return entry, Pn, hops, err
}

// iterativeSearch implements the Kademlia α-concurrent iterative lookup algorithm
// (§2.3 of the original Maymounkov-Mazières paper), shared by IterativeFindNode
// and IterativeFindValue.
//
// Algorithm:
//  1. Seed the candidate set Pn with the caller-supplied seeds.
//  2. Each round: pick up to α unqueried contacts, call roundFn (issues parallel RPCs).
//  3. roundFn returns newly discovered contacts (merged into Pn) and an optional found
//     value entry. A non-nil entry causes an immediate return.
//  4. Re-sort Pn by XOR distance; trim to k.
//  5. Convergence: if the closest node has not improved, run a final round over all
//     remaining unqueried contacts, then stop.
//
// roundFn receives: the per-round context (already deadline-limited), the current
// batch, the hop index, and a pointer to the hops slice to append into.
// It returns (newContacts, foundEntry) where foundEntry != nil signals early exit.
func (n *KademliaNode) iterativeSearch(
	ctx context.Context,
	target NodeID,
	seeds []Contact,
	roundFn func(alphaCtx context.Context, batch []Contact, hopIdx int, hops *[]transport.HopEvent) ([]Contact, *store.ValueEntry),
) (*store.ValueEntry, []Contact, []transport.HopEvent, error) {
	alpha := n.Config.Alpha
	k := n.Config.K

	Pn := make([]Contact, len(seeds))
	copy(Pn, seeds)

	queried := make(map[NodeID]bool)
	queried[n.ID] = true // never query self

	var hops []transport.HopEvent
	hopIdx := 0

	var closestSeen NodeID
	if len(Pn) > 0 {
		closestSeen = Pn[0].ID
	}

	mergeContacts := func(newContacts []Contact) {
		for _, c := range newContacts {
			if c.ID == n.ID {
				continue
			}
			n.updateRoutingTable(c)
			found := false
			for _, existing := range Pn {
				if existing.ID == c.ID {
					found = true
					break
				}
			}
			if !found {
				Pn = append(Pn, c)
			}
		}
		sort.Slice(Pn, func(i, j int) bool {
			return XORDistance(Pn[i].ID, target).Cmp(XORDistance(Pn[j].ID, target)) < 0
		})
		if len(Pn) > k {
			Pn = Pn[:k]
		}
	}

	for {
		// Pick up to α unqueried contacts.
		var batch []Contact
		for _, c := range Pn {
			if !queried[c.ID] {
				batch = append(batch, c)
				if len(batch) >= alpha {
					break
				}
			}
		}
		if len(batch) == 0 {
			break
		}
		for _, c := range batch {
			queried[c.ID] = true
		}

		alphaCtx, alphaCancel := context.WithTimeout(ctx, n.Config.AlphaTimeout)
		newContacts, foundEntry := roundFn(alphaCtx, batch, hopIdx, &hops)
		alphaCancel()
		hopIdx++

		if foundEntry != nil {
			return foundEntry, Pn, hops, nil
		}

		mergeContacts(newContacts)

		if len(Pn) == 0 {
			break
		}

		// Convergence check: if the closest node has not improved, run a
		// final round over all remaining unqueried contacts, then stop.
		newClosest := Pn[0].ID
		if newClosest == closestSeen {
			var finalBatch []Contact
			for _, c := range Pn {
				if !queried[c.ID] {
					finalBatch = append(finalBatch, c)
					queried[c.ID] = true
				}
			}
			if len(finalBatch) > 0 {
				finalCtx, finalCancel := context.WithTimeout(ctx, n.Config.AlphaTimeout)
				newContacts, foundEntry = roundFn(finalCtx, finalBatch, hopIdx, &hops)
				finalCancel()
				if foundEntry != nil {
					return foundEntry, Pn, hops, nil
				}
				mergeContacts(newContacts)
			}
			break
		}
		closestSeen = newClosest
	}

	return nil, Pn, hops, nil
}

func (n *KademliaNode) runIterativeRound(
	batch []Contact,
	target NodeID,
	mechanismPrefix string,
	hopIdx int,
	hops *[]transport.HopEvent,
	rpc func(contact Contact) iterativeRoundResult,
) <-chan iterativeRoundResult {
	results := make(chan iterativeRoundResult, len(batch))
	var wg sync.WaitGroup
	for _, c := range batch {
		wg.Add(1)
		go func(contact Contact) {
			defer wg.Done()
			results <- rpc(contact)
		}(c)
	}
	wg.Wait()
	close(results)

	n.emitLookupHop(target, fmt.Sprintf("%s_alpha%d", mechanismPrefix, len(batch)), hopIdx, hops)
	return results
}

func (n *KademliaNode) emitLookupHop(target NodeID, mechanism string, hopIdx int, hops *[]transport.HopEvent) {
	hop := transport.HopEvent{
		FromNode:  consistent.IDToHex([20]byte(n.ID)),
		ToNode:    consistent.IDToHex([20]byte(target)),
		Mechanism: mechanism,
		HopIndex:  hopIdx,
	}
	*hops = append(*hops, hop)
	n.Bus.Publish(events.MakeEvent(events.EventLookupHop, events.LookupHopPayload{
		FromNode:  hop.FromNode,
		ToNode:    hop.ToNode,
		Mechanism: hop.Mechanism,
		HopIndex:  hop.HopIndex,
	}))
}

func nodeRefsToContacts(refs []transport.NodeRef) []Contact {
	contacts := make([]Contact, 0, len(refs))
	for _, nr := range refs {
		contacts = append(contacts, Contact{
			ID:       NodeID(nr.ID),
			Addr:     nr.Addr,
			LastSeen: time.Now(),
		})
	}
	return contacts
}

// StoreValue stores a key-value pair on the k closest nodes.
func (n *KademliaNode) StoreValue(ctx context.Context, key string, value []byte) error {
	keyID := NodeID(consistent.KeyID(key))
	entry := &store.ValueEntry{
		Key:       [20]byte(keyID),
		Value:     value,
		Timestamp: time.Now(),
		TTL:       n.Config.ExpireTTL,
		NodeID:    consistent.IDToHex([20]byte(n.ID)),
	}

	// Store locally first.
	n.KVStore.Put(entry)
	if n.antiEntropy != nil {
		n.antiEntropy.NotifyPut(entry.Key, entry.Value)
	}

	// Find k closest nodes and store on them.
	contacts, _, err := n.IterativeFindNode(ctx, keyID)
	if err != nil {
		return fmt.Errorf("kademlia.Store FIND_NODE: %w", err)
	}

	for _, c := range contacts {
		ref := transport.NodeRef{ID: [20]byte(c.ID), Addr: c.Addr}
		_ = n.Transport.KStore(ctx, n.Ref(), ref, entry)
	}
	return nil
}
