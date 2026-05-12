// Package consistent implements the consistent hashing ring primitives for the DHT system.
// All operations work on a 2^160 key space using SHA-1 hash functions.
//
// S/Kademlia extension: MineNodeID performs proof-of-work node ID generation
// by finding a nonce such that SHA-256(nonce) has the required number of leading
// zero bits, preventing Sybil attacks (Baumgart & Mies, 2007).
package consistent

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math/big"
)

// keySpaceBits is the number of bits in the hash space (SHA-1 = 160 bits).
const keySpaceBits = 160

// keySpaceSize = 2^160
var keySpaceSize = new(big.Int).Exp(big.NewInt(2), big.NewInt(keySpaceBits), nil)

// SHA1 returns the SHA-1 hash of the input as a 20-byte array.
func SHA1(input string) [20]byte {
	return sha1.Sum([]byte(input))
}

// NodeIDFromAddr derives the node ID from an address string (IP:port).
func NodeIDFromAddr(addr string) [20]byte {
	return SHA1(addr)
}

// KeyID derives the key ID from a key string.
func KeyID(key string) [20]byte {
	return SHA1(key)
}

// IDToBigInt converts a [20]byte ID to a *big.Int (big-endian unsigned).
func IDToBigInt(id [20]byte) *big.Int {
	return new(big.Int).SetBytes(id[:])
}

// BigIntToID converts a *big.Int to a [20]byte ID (big-endian, zero-padded).
func BigIntToID(n *big.Int) [20]byte {
	// Ensure we work with n mod 2^160
	n = new(big.Int).Mod(n, keySpaceSize)
	var id [20]byte
	b := n.Bytes()
	// Right-align (big-endian) in the 20-byte array
	copy(id[20-len(b):], b)
	return id
}

// IsInInterval returns true if id ∈ (start, end) exclusive on the ring.
// Handles the wrap-around case where start > end (mod 2^160).
func IsInInterval(id, start, end [20]byte) bool {
	if start == end {
		// Degenerate: interval covers nothing (if caller means full ring, use different check)
		return false
	}
	idInt := IDToBigInt(id)
	startInt := IDToBigInt(start)
	endInt := IDToBigInt(end)

	if startInt.Cmp(endInt) < 0 {
		// Normal (non-wrapping) interval: start < end
		return idInt.Cmp(startInt) > 0 && idInt.Cmp(endInt) < 0
	}
	// Wrapping interval: start > end
	// id ∈ (start, end) wrapping means id > start OR id < end
	return idInt.Cmp(startInt) > 0 || idInt.Cmp(endInt) < 0
}

// IsInIntervalRightClosed returns true if id ∈ (start, end] (inclusive end) on the ring.
func IsInIntervalRightClosed(id, start, end [20]byte) bool {
	if id == end {
		return true
	}
	return IsInInterval(id, start, end)
}

// Add computes (id + n) mod 2^160.
func Add(id [20]byte, n *big.Int) [20]byte {
	result := new(big.Int).Add(IDToBigInt(id), n)
	result.Mod(result, keySpaceSize)
	return BigIntToID(result)
}

// PowerOfTwo computes 2^i as a *big.Int.
func PowerOfTwo(i int) *big.Int {
	return new(big.Int).Lsh(big.NewInt(1), uint(i))
}

// IDToHex encodes an ID to a lowercase hex string.
func IDToHex(id [20]byte) string {
	return hex.EncodeToString(id[:])
}

// IDFromHex decodes a hex string to a [20]byte ID.
func IDFromHex(s string) ([20]byte, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return [20]byte{}, err
	}
	var id [20]byte
	copy(id[20-len(b):], b)
	return id, nil
}

// IDToFloat64 returns the position of an ID as a float in [0, 1).
// Used for ring visualization.
func IDToFloat64(id [20]byte) float64 {
	n := IDToBigInt(id)
	// Convert to float: n / 2^160
	f := new(big.Float).SetInt(n)
	max := new(big.Float).SetInt(keySpaceSize)
	result, _ := new(big.Float).Quo(f, max).Float64()
	return result
}

// Compare compares two IDs numerically.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func Compare(a, b [20]byte) int {
	return IDToBigInt(a).Cmp(IDToBigInt(b))
}

// MineNodeID implements S/Kademlia proof-of-work node ID generation.
// NOTE: This function is available for use but is NOT called during normal node creation
// to avoid startup latency. Set difficulty=0 for no PoW. Typical values are 8-20 for
// production Sybil resistance.
// It searches for a 64-bit nonce such that SHA-256(nonce) has at least
// `difficulty` leading zero bits. The node ID is then SHA-1(SHA-256(nonce)).
// This makes Sybil attacks computationally expensive (expected 2^difficulty hashes).
//
// Returns the mined node ID and the winning nonce.
// A difficulty of 0 returns immediately; typical values are 8–20.
func MineNodeID(difficulty int) ([20]byte, uint64) {
	if difficulty <= 0 {
		var nonce uint64
		h := sha256.Sum256(make([]byte, 8))
		return sha1.Sum(h[:]), nonce
	}

	// Precompute the bitmask for the leading difficulty bits of the first byte(s).
	// We check byte-by-byte for efficiency.
	fullBytes := difficulty / 8
	remainBits := difficulty % 8

	var nonceBuf [8]byte
	for nonce := uint64(0); ; nonce++ {
		binary.BigEndian.PutUint64(nonceBuf[:], nonce)
		h := sha256.Sum256(nonceBuf[:])

		// Check leading zero bytes.
		valid := true
		for i := 0; i < fullBytes; i++ {
			if h[i] != 0 {
				valid = false
				break
			}
		}
		if valid && remainBits > 0 && fullBytes < 32 {
			mask := byte(0xFF << (8 - remainBits))
			if h[fullBytes]&mask != 0 {
				valid = false
			}
		}
		if valid {
			nodeID := sha1.Sum(h[:])
			return nodeID, nonce
		}
	}
}
