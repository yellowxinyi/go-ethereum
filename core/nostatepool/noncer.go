package nostatepool

import (
	"sync"

	"github.com/ethereum/go-ethereum/common"
)

// noncer is a virtual nonce tracker used by the no-state pool.
// It never falls back to chain state and only tracks values learned in-pool.
type noncer struct {
	nonces map[common.Address]uint64
	base   map[common.Address]uint64
	lock   sync.Mutex
}

func newNoncer() *noncer {
	return &noncer{
		nonces: make(map[common.Address]uint64),
		base:   make(map[common.Address]uint64),
	}
}

func (txn *noncer) get(addr common.Address) uint64 {
	txn.lock.Lock()
	defer txn.lock.Unlock()
	if nonce, ok := txn.nonces[addr]; ok {
		return nonce
	}
	return txn.base[addr]
}

func (txn *noncer) set(addr common.Address, nonce uint64) {
	txn.lock.Lock()
	defer txn.lock.Unlock()
	txn.nonces[addr] = nonce
}

func (txn *noncer) setIfLower(addr common.Address, nonce uint64) {
	txn.lock.Lock()
	defer txn.lock.Unlock()
	if current, ok := txn.nonces[addr]; ok && current <= nonce {
		return
	}
	txn.nonces[addr] = nonce
}

func (txn *noncer) setBaseIfLower(addr common.Address, nonce uint64) {
	txn.lock.Lock()
	defer txn.lock.Unlock()
	if current, ok := txn.base[addr]; ok && current <= nonce {
		return
	}
	txn.base[addr] = nonce
}

func (txn *noncer) setBaseAll(all map[common.Address]uint64) {
	txn.lock.Lock()
	defer txn.lock.Unlock()
	txn.base = all
}

func (txn *noncer) setAll(all map[common.Address]uint64) {
	txn.lock.Lock()
	defer txn.lock.Unlock()
	txn.nonces = all
}
