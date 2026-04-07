// Copyright 2024 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package state

import (
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/overlay"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/bintrie"
	"github.com/ethereum/go-ethereum/trie/transitiontrie"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/database"
)

// ContractCodeReader defines the interface for accessing contract code.
//
// ContractCodeReader is supposed to be thread-safe.
type ContractCodeReader interface {
	// Has returns the flag indicating whether the contract code with
	// specified address and hash exists or not.
	Has(addr common.Address, codeHash common.Hash) bool

	// Code retrieves a particular contract's code. Returns nil code if the
	// requested contract code doesn't exist.
	Code(addr common.Address, codeHash common.Hash) []byte

	// CodeSize retrieves a particular contracts code's size. Returns zero code
	// size if the requested contract code doesn't exist.
	CodeSize(addr common.Address, codeHash common.Hash) int
}

// StateReader defines the interface for accessing accounts and storage slots
// associated with a specific state.
//
// StateReader is supposed to be thread-safe.
type StateReader interface {
	// Account retrieves the account associated with a particular address.
	//
	// - Returns a nil account if it does not exist
	// - Returns an error only if an unexpected issue occurs
	// - The returned account is safe to modify after the call
	Account(addr common.Address) (*types.StateAccount, error)

	// Storage retrieves the storage slot associated with a particular account
	// address and slot key.
	//
	// - Returns an empty slot if it does not exist
	// - Returns an error only if an unexpected issue occurs
	// - The returned storage slot is safe to modify after the call
	Storage(addr common.Address, slot common.Hash) (common.Hash, error)
}

// Reader defines the interface for accessing accounts, storage slots and contract
// code associated with a specific state.
//
// Reader is assumed to be thread-safe and implementation must take care of the
// concurrency issue by themselves.
type Reader interface {
	ContractCodeReader
	StateReader
}

// flatReader wraps a database state reader and is safe for concurrent access.
type flatReader struct {
	reader database.StateReader
}

// newFlatReader constructs a state reader with on the given state root.
func newFlatReader(reader database.StateReader) *flatReader {
	return &flatReader{reader: reader}
}

// plainReader wraps a KV reader and is safe for concurrent access.
// It reads state from the plain (address/slot) KV view.
type plainReader struct {
	db ethdb.KeyValueReader
}

// newPlainReader constructs a plain state reader backed by rawdb flat-KV accessors.
func newPlainReader(db ethdb.KeyValueReader) *plainReader {
	return &plainReader{db: db}
}

// Account implements StateReader, retrieving the account specified by the address.
//
// An error will be returned if the associated snapshot is already stale or
// the requested account is not yet covered by the snapshot.
//
// The returned account might be nil if it's not existent.
func (r *flatReader) Account(addr common.Address) (*types.StateAccount, error) {
	account, err := r.reader.Account(crypto.Keccak256Hash(addr[:]))
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, nil
	}
	acct := &types.StateAccount{
		Nonce:    account.Nonce,
		Balance:  account.Balance,
		CodeHash: account.CodeHash,
		Root:     common.BytesToHash(account.Root),
	}
	if len(acct.CodeHash) == 0 {
		acct.CodeHash = types.EmptyCodeHash.Bytes()
	}
	if acct.Root == (common.Hash{}) {
		acct.Root = types.EmptyRootHash
	}
	return acct, nil
}

// Storage implements StateReader, retrieving the storage slot specified by the
// address and slot key.
//
// An error will be returned if the associated snapshot is already stale or
// the requested storage slot is not yet covered by the snapshot.
//
// The returned storage slot might be empty if it's not existent.
func (r *flatReader) Storage(addr common.Address, key common.Hash) (common.Hash, error) {
	addrHash := crypto.Keccak256Hash(addr[:])
	slotHash := crypto.Keccak256Hash(key[:])
	ret, err := r.reader.Storage(addrHash, slotHash)
	if err != nil {
		return common.Hash{}, err
	}
	if len(ret) == 0 {
		return common.Hash{}, nil
	}
	// Perform the rlp-decode as the slot value is RLP-encoded in the state
	// snapshot.
	_, content, _, err := rlp.Split(ret)
	if err != nil {
		return common.Hash{}, err
	}
	var value common.Hash
	value.SetBytes(content)
	return value, nil
}

// Account implements StateReader for plain-KV, retrieving the account specified by address.
func (r *plainReader) Account(addr common.Address) (*types.StateAccount, error) {
	data := rawdb.ReadPlainAccount(r.db, addr)
	if len(data) == 0 {
		// Fallback to hashed view if plain state is not yet populated.
		addrHash := crypto.Keccak256Hash(addr.Bytes())
		data = rawdb.ReadHashedAccount(r.db, addrHash)
	}
	if len(data) == 0 {
		return nil, nil
	}
	var account types.StateAccount
	if err := rlp.DecodeBytes(data, &account); err != nil {
		return nil, err
	}
	// Normalize empty fields to consensus defaults.
	if len(account.CodeHash) == 0 {
		account.CodeHash = types.EmptyCodeHash.Bytes()
	}
	if account.Root == (common.Hash{}) {
		account.Root = types.EmptyRootHash
	}
	return &account, nil
}

// Storage implements StateReader for plain-KV, retrieving the storage slot specified by address and key.
func (r *plainReader) Storage(addr common.Address, key common.Hash) (common.Hash, error) {
	incarnation, _ := rawdb.ReadPlainIncarnation(r.db, addr)
	data := rawdb.ReadPlainStorage(r.db, addr, incarnation, key)
	if len(data) == 0 {
		// Fallback to hashed view if plain state is not yet populated.
		addrHash := crypto.Keccak256Hash(addr.Bytes())
		hashedIncarnation, _ := rawdb.ReadHashedIncarnation(r.db, addrHash)
		slotHash := crypto.Keccak256Hash(key.Bytes())
		data = rawdb.ReadHashedStorage(r.db, addrHash, hashedIncarnation, slotHash)
	}
	if len(data) == 0 {
		return common.Hash{}, nil
	}
	// Prefer raw 32-byte storage values; fall back to RLP-decoding if needed.
	if len(data) == common.HashLength {
		return common.BytesToHash(data), nil
	}
	_, content, _, err := rlp.Split(data)
	if err != nil {
		return common.Hash{}, err
	}
	var value common.Hash
	value.SetBytes(content)
	return value, nil
}

type overlayEntry struct {
	value   []byte
	existed bool
}

// OverlayReader provides a read-only overlay on top of the flat-KV state.
// It is intended for temporary reorg/fork execution views.
type OverlayReader struct {
	db      ethdb.KeyValueReader
	lock    sync.RWMutex
	entries map[string]overlayEntry
}

// NewOverlayReader constructs an overlay reader backed by the provided db.
func NewOverlayReader(db ethdb.KeyValueReader) *OverlayReader {
	return &OverlayReader{
		db:      db,
		entries: make(map[string]overlayEntry),
	}
}

// SetRaw records an overlay value for the given raw key.
func (r *OverlayReader) SetRaw(key []byte, value []byte, existed bool) {
	if len(key) == 0 {
		return
	}
	var copyVal []byte
	if len(value) > 0 {
		copyVal = append([]byte(nil), value...)
	}
	r.lock.Lock()
	r.entries[string(key)] = overlayEntry{value: copyVal, existed: existed}
	r.lock.Unlock()
}

func (r *OverlayReader) raw(key []byte) (overlayEntry, bool) {
	r.lock.RLock()
	entry, ok := r.entries[string(key)]
	r.lock.RUnlock()
	return entry, ok
}

func (r *OverlayReader) readIncarnationPlain(addr common.Address) (uint64, bool) {
	if entry, ok := r.raw(rawdb.PlainIncarnationKey(addr)); ok {
		if !entry.existed || len(entry.value) == 0 {
			return 0, false
		}
		if len(entry.value) != 8 {
			return 0, false
		}
		return binary.BigEndian.Uint64(entry.value), true
	}
	return rawdb.ReadPlainIncarnation(r.db, addr)
}

func (r *OverlayReader) readIncarnationHashed(addrHash common.Hash) (uint64, bool) {
	if entry, ok := r.raw(rawdb.HashedIncarnationKey(addrHash)); ok {
		if !entry.existed || len(entry.value) == 0 {
			return 0, false
		}
		if len(entry.value) != 8 {
			return 0, false
		}
		return binary.BigEndian.Uint64(entry.value), true
	}
	return rawdb.ReadHashedIncarnation(r.db, addrHash)
}

func (r *OverlayReader) decodeAccount(data []byte) (*types.StateAccount, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var account types.StateAccount
	if err := rlp.DecodeBytes(data, &account); err != nil {
		return nil, err
	}
	if len(account.CodeHash) == 0 {
		account.CodeHash = types.EmptyCodeHash.Bytes()
	}
	if account.Root == (common.Hash{}) {
		account.Root = types.EmptyRootHash
	}
	return &account, nil
}

// Account implements StateReader with overlay support.
func (r *OverlayReader) Account(addr common.Address) (*types.StateAccount, error) {
	if entry, ok := r.raw(rawdb.PlainAccountKey(addr)); ok {
		if entry.existed {
			return r.decodeAccount(entry.value)
		}
	} else {
		data := rawdb.ReadPlainAccount(r.db, addr)
		if len(data) > 0 {
			return r.decodeAccount(data)
		}
	}
	addrHash := crypto.Keccak256Hash(addr.Bytes())
	if entry, ok := r.raw(rawdb.HashedAccountKey(addrHash)); ok {
		if !entry.existed {
			return nil, nil
		}
		return r.decodeAccount(entry.value)
	}
	data := rawdb.ReadHashedAccount(r.db, addrHash)
	return r.decodeAccount(data)
}

func (r *OverlayReader) decodeStorage(data []byte) (common.Hash, error) {
	if len(data) == 0 {
		return common.Hash{}, nil
	}
	if len(data) == common.HashLength {
		return common.BytesToHash(data), nil
	}
	_, content, _, err := rlp.Split(data)
	if err != nil {
		return common.Hash{}, err
	}
	var value common.Hash
	value.SetBytes(content)
	return value, nil
}

// Storage implements StateReader with overlay support.
func (r *OverlayReader) Storage(addr common.Address, key common.Hash) (common.Hash, error) {
	if inc, ok := r.readIncarnationPlain(addr); ok {
		plainKey := rawdb.PlainStorageKey(addr, inc, key)
		if entry, ok := r.raw(plainKey); ok {
			if !entry.existed {
				goto hashed
			}
			return r.decodeStorage(entry.value)
		}
		data := rawdb.ReadPlainStorage(r.db, addr, inc, key)
		if len(data) > 0 {
			return r.decodeStorage(data)
		}
	}

hashed:
	addrHash := crypto.Keccak256Hash(addr.Bytes())
	inc, ok := r.readIncarnationHashed(addrHash)
	if !ok {
		return common.Hash{}, nil
	}
	slotHash := crypto.Keccak256Hash(key.Bytes())
	hashedKey := rawdb.HashedStorageKey(addrHash, inc, slotHash)
	if entry, ok := r.raw(hashedKey); ok {
		if !entry.existed {
			return common.Hash{}, nil
		}
		return r.decodeStorage(entry.value)
	}
	data := rawdb.ReadHashedStorage(r.db, addrHash, inc, slotHash)
	return r.decodeStorage(data)
}

// Code implements ContractCodeReader with overlay support.
func (r *OverlayReader) Code(addr common.Address, codeHash common.Hash) ([]byte, error) {
	key := append(rawdb.CodePrefix, codeHash.Bytes()...)
	if entry, ok := r.raw(key); ok {
		if !entry.existed {
			return nil, nil
		}
		return append([]byte(nil), entry.value...), nil
	}
	code := rawdb.ReadCode(r.db, codeHash)
	if len(code) == 0 {
		return nil, nil
	}
	return code, nil
}

// CodeSize implements ContractCodeReader with overlay support.
func (r *OverlayReader) CodeSize(addr common.Address, codeHash common.Hash) (int, error) {
	code, err := r.Code(addr, codeHash)
	if err != nil {
		return 0, err
	}
	return len(code), nil
}

// trieReader implements the StateReader interface, providing functions to access
// state from the referenced trie.
//
// trieReader is safe for concurrent read.
type trieReader struct {
	root common.Hash      // State root which uniquely represent a state
	db   *triedb.Database // Database for loading trie

	// Main trie, resolved in constructor. Note either the Merkle-Patricia-tree
	// or Verkle-tree is not safe for concurrent read.
	mainTrie Trie

	subRoots map[common.Address]common.Hash // Set of storage roots, cached when the account is resolved
	subTries map[common.Address]Trie        // Group of storage tries, cached when it's resolved
	lock     sync.Mutex                     // Lock for protecting concurrent read
}

// newTrieReader constructs a trie reader of the specific state. An error will be
// returned if the associated trie specified by root is not existent.
func newTrieReader(root common.Hash, db *triedb.Database) (*trieReader, error) {
	var (
		tr  Trie
		err error
	)
	if !db.IsVerkle() {
		tr, err = trie.NewStateTrie(trie.StateTrieID(root), db)
	} else {
		// When IsVerkle() is true, create a BinaryTrie wrapped in TransitionTrie
		binTrie, binErr := bintrie.NewBinaryTrie(root, db)
		if binErr != nil {
			return nil, binErr
		}

		// Based on the transition status, determine if the overlay
		// tree needs to be created, or if a single, target tree is
		// to be picked.
		ts := overlay.LoadTransitionState(db.Disk(), root, true)
		if ts.InTransition() {
			mpt, err := trie.NewStateTrie(trie.StateTrieID(ts.BaseRoot), db)
			if err != nil {
				return nil, err
			}
			tr = transitiontrie.NewTransitionTrie(mpt, binTrie, false)
		} else {
			// HACK: Use TransitionTrie with nil base as a wrapper to make BinaryTrie
			// satisfy the Trie interface. This works around the import cycle between
			// trie and trie/bintrie packages.
			//
			// TODO: In future PRs, refactor the package structure to avoid this hack:
			// - Option 1: Move common interfaces (Trie, NodeIterator) to a separate
			//   package that both trie and trie/bintrie can import
			// - Option 2: Create a factory function in the trie package that returns
			//   BinaryTrie as a Trie interface without direct import
			// - Option 3: Move BinaryTrie to the main trie package
			//
			// The current approach works but adds unnecessary overhead and complexity
			// by using TransitionTrie when there's no actual transition happening.
			tr = transitiontrie.NewTransitionTrie(nil, binTrie, false)
		}
	}
	if err != nil {
		return nil, err
	}
	return &trieReader{
		root:     root,
		db:       db,
		mainTrie: tr,
		subRoots: make(map[common.Address]common.Hash),
		subTries: make(map[common.Address]Trie),
	}, nil
}

// account is the inner version of Account and assumes the r.lock is already held.
func (r *trieReader) account(addr common.Address) (*types.StateAccount, error) {
	account, err := r.mainTrie.GetAccount(addr)
	if err != nil {
		return nil, err
	}
	if account == nil {
		r.subRoots[addr] = types.EmptyRootHash
	} else {
		r.subRoots[addr] = account.Root
	}
	return account, nil
}

// Account implements StateReader, retrieving the account specified by the address.
//
// An error will be returned if the trie state is corrupted. An nil account
// will be returned if it's not existent in the trie.
func (r *trieReader) Account(addr common.Address) (*types.StateAccount, error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	return r.account(addr)
}

// Storage implements StateReader, retrieving the storage slot specified by the
// address and slot key.
//
// An error will be returned if the trie state is corrupted. An empty storage
// slot will be returned if it's not existent in the trie.
func (r *trieReader) Storage(addr common.Address, key common.Hash) (common.Hash, error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	var (
		tr    Trie
		found bool
		value common.Hash
	)
	if r.db.IsVerkle() {
		tr = r.mainTrie
	} else {
		tr, found = r.subTries[addr]
		if !found {
			root, ok := r.subRoots[addr]

			// The storage slot is accessed without account caching. It's unexpected
			// behavior but try to resolve the account first anyway.
			if !ok {
				_, err := r.account(addr)
				if err != nil {
					return common.Hash{}, err
				}
				root = r.subRoots[addr]
			}
			var err error
			tr, err = trie.NewStateTrie(trie.StorageTrieID(r.root, crypto.Keccak256Hash(addr.Bytes()), root), r.db)
			if err != nil {
				return common.Hash{}, err
			}
			r.subTries[addr] = tr
		}
	}
	ret, err := tr.GetStorage(addr, key.Bytes())
	if err != nil {
		return common.Hash{}, err
	}
	value.SetBytes(ret)
	return value, nil
}

// multiStateReader is the aggregation of a list of StateReader interface,
// providing state access by leveraging all readers. The checking priority
// is determined by the position in the reader list.
//
// multiStateReader is safe for concurrent read and assumes all underlying
// readers are thread-safe as well.
type multiStateReader struct {
	readers []StateReader // List of state readers, sorted by checking priority
}

// newMultiStateReader constructs a multiStateReader instance with the given
// readers. The priority among readers is assumed to be sorted. Note, it must
// contain at least one reader for constructing a multiStateReader.
func newMultiStateReader(readers ...StateReader) (*multiStateReader, error) {
	if len(readers) == 0 {
		return nil, errors.New("empty reader set")
	}
	return &multiStateReader{
		readers: readers,
	}, nil
}

// Account implementing StateReader interface, retrieving the account associated
// with a particular address.
//
// - Returns a nil account if it does not exist
// - Returns an error only if an unexpected issue occurs
// - The returned account is safe to modify after the call
func (r *multiStateReader) Account(addr common.Address) (*types.StateAccount, error) {
	var errs []error
	for _, reader := range r.readers {
		acct, err := reader.Account(addr)
		if err == nil {
			return acct, nil
		}
		errs = append(errs, err)
	}
	return nil, errors.Join(errs...)
}

// Storage implementing StateReader interface, retrieving the storage slot
// associated with a particular account address and slot key.
//
// - Returns an empty slot if it does not exist
// - Returns an error only if an unexpected issue occurs
// - The returned storage slot is safe to modify after the call
func (r *multiStateReader) Storage(addr common.Address, slot common.Hash) (common.Hash, error) {
	var errs []error
	for _, reader := range r.readers {
		slot, err := reader.Storage(addr, slot)
		if err == nil {
			return slot, nil
		}
		errs = append(errs, err)
	}
	return common.Hash{}, errors.Join(errs...)
}

// stateReaderWithCache is a wrapper around StateReader that maintains additional
// state caches to support concurrent state access.
type stateReaderWithCache struct {
	StateReader

	// Previously resolved state entries.
	accounts    map[common.Address]*types.StateAccount
	accountLock sync.RWMutex

	// List of storage buckets, each of which is thread-safe.
	// This reader is typically used in scenarios requiring concurrent
	// access to storage. Using multiple buckets helps mitigate
	// the overhead caused by locking.
	storageBuckets [16]struct {
		lock     sync.RWMutex
		storages map[common.Address]map[common.Hash]common.Hash
	}
}

// newStateReaderWithCache constructs the state reader with local cache.
func newStateReaderWithCache(sr StateReader) *stateReaderWithCache {
	r := &stateReaderWithCache{
		StateReader: sr,
		accounts:    make(map[common.Address]*types.StateAccount),
	}
	for i := range r.storageBuckets {
		r.storageBuckets[i].storages = make(map[common.Address]map[common.Hash]common.Hash)
	}
	return r
}

// account retrieves the account specified by the address along with a flag
// indicating whether it's found in the cache or not. The returned account
// might be nil if it's not existent.
//
// An error will be returned if the state is corrupted in the underlying reader.
func (r *stateReaderWithCache) account(addr common.Address) (*types.StateAccount, bool, error) {
	// Try to resolve the requested account in the local cache
	r.accountLock.RLock()
	acct, ok := r.accounts[addr]
	r.accountLock.RUnlock()
	if ok {
		return acct, true, nil
	}
	// Try to resolve the requested account from the underlying reader
	acct, err := r.StateReader.Account(addr)
	if err != nil {
		return nil, false, err
	}
	r.accountLock.Lock()
	r.accounts[addr] = acct
	r.accountLock.Unlock()
	return acct, false, nil
}

// Account implements StateReader, retrieving the account specified by the address.
// The returned account might be nil if it's not existent.
//
// An error will be returned if the state is corrupted in the underlying reader.
func (r *stateReaderWithCache) Account(addr common.Address) (*types.StateAccount, error) {
	account, _, err := r.account(addr)
	return account, err
}

// storage retrieves the storage slot specified by the address and slot key, along
// with a flag indicating whether it's found in the cache or not. The returned
// storage slot might be empty if it's not existent.
func (r *stateReaderWithCache) storage(addr common.Address, slot common.Hash) (common.Hash, bool, error) {
	var (
		value  common.Hash
		ok     bool
		bucket = &r.storageBuckets[addr[0]&0x0f]
	)
	// Try to resolve the requested storage slot in the local cache
	bucket.lock.RLock()
	slots, ok := bucket.storages[addr]
	if ok {
		value, ok = slots[slot]
	}
	bucket.lock.RUnlock()
	if ok {
		return value, true, nil
	}
	// Try to resolve the requested storage slot from the underlying reader
	value, err := r.StateReader.Storage(addr, slot)
	if err != nil {
		return common.Hash{}, false, err
	}
	bucket.lock.Lock()
	slots, ok = bucket.storages[addr]
	if !ok {
		slots = make(map[common.Hash]common.Hash)
		bucket.storages[addr] = slots
	}
	slots[slot] = value
	bucket.lock.Unlock()

	return value, false, nil
}

// Storage implements StateReader, retrieving the storage slot specified by the
// address and slot key. The returned storage slot might be empty if it's not
// existent.
//
// An error will be returned if the state is corrupted in the underlying reader.
func (r *stateReaderWithCache) Storage(addr common.Address, slot common.Hash) (common.Hash, error) {
	value, _, err := r.storage(addr, slot)
	return value, err
}

// stateReaderWithStats is a wrapper over the stateReaderWithCache, tracking
// the cache hit statistics of the reader.
type stateReaderWithStats struct {
	*stateReaderWithCache

	accountCacheHit  atomic.Int64
	accountCacheMiss atomic.Int64
	storageCacheHit  atomic.Int64
	storageCacheMiss atomic.Int64
}

// newReaderWithStats constructs the state reader with additional statistics tracked.
func newStateReaderWithStats(sr *stateReaderWithCache) *stateReaderWithStats {
	return &stateReaderWithStats{
		stateReaderWithCache: sr,
	}
}

// Account implements StateReader, retrieving the account specified by the address.
// The returned account might be nil if it's not existent.
//
// An error will be returned if the state is corrupted in the underlying reader.
func (r *stateReaderWithStats) Account(addr common.Address) (*types.StateAccount, error) {
	account, incache, err := r.stateReaderWithCache.account(addr)
	if err != nil {
		return nil, err
	}
	if incache {
		r.accountCacheHit.Add(1)
	} else {
		r.accountCacheMiss.Add(1)
	}
	return account, nil
}

// Storage implements StateReader, retrieving the storage slot specified by the
// address and slot key. The returned storage slot might be empty if it's not
// existent.
//
// An error will be returned if the state is corrupted in the underlying reader.
func (r *stateReaderWithStats) Storage(addr common.Address, slot common.Hash) (common.Hash, error) {
	value, incache, err := r.stateReaderWithCache.storage(addr, slot)
	if err != nil {
		return common.Hash{}, err
	}
	if incache {
		r.storageCacheHit.Add(1)
	} else {
		r.storageCacheMiss.Add(1)
	}
	return value, nil
}

// GetStateStats implements StateReaderStater, returning the statistics of the
// state reader.
func (r *stateReaderWithStats) GetStateStats() StateReaderStats {
	return StateReaderStats{
		AccountCacheHit:  r.accountCacheHit.Load(),
		AccountCacheMiss: r.accountCacheMiss.Load(),
		StorageCacheHit:  r.storageCacheHit.Load(),
		StorageCacheMiss: r.storageCacheMiss.Load(),
	}
}

// reader aggregates a code reader and a state reader into a single object.
type reader struct {
	ContractCodeReader
	StateReader
}

// newReader constructs a reader with the supplied code reader and state reader.
func newReader(codeReader ContractCodeReader, stateReader StateReader) *reader {
	return &reader{
		ContractCodeReader: codeReader,
		StateReader:        stateReader,
	}
}

// GetCodeStats returns the statistics of code access.
func (r *reader) GetCodeStats() ContractCodeReaderStats {
	if stater, ok := r.ContractCodeReader.(ContractCodeReaderStater); ok {
		return stater.GetCodeStats()
	}
	return ContractCodeReaderStats{}
}

// GetStateStats returns the statistics of state access.
func (r *reader) GetStateStats() StateReaderStats {
	if stater, ok := r.StateReader.(StateReaderStater); ok {
		return stater.GetStateStats()
	}
	return StateReaderStats{}
}

// GetStats returns the aggregated statistics for both state and code access.
func (r *reader) GetStats() ReaderStats {
	return ReaderStats{
		CodeStats:  r.GetCodeStats(),
		StateStats: r.GetStateStats(),
	}
}
