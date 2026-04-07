// Copyright 2026 The go-ethereum Authors
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

package rawdb

import (
	"encoding/binary"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

// plainAccountKey = PlainAccountPrefix + address
func plainAccountKey(address common.Address) []byte {
	return append(PlainAccountPrefix, address.Bytes()...)
}

// plainStorageKey = PlainStoragePrefix + address + incarnation + slot
func plainStorageKey(address common.Address, incarnation uint64, slot common.Hash) []byte {
	buf := make([]byte, len(PlainStoragePrefix)+common.AddressLength+8+common.HashLength)
	n := copy(buf, PlainStoragePrefix)
	n += copy(buf[n:], address.Bytes())
	binary.BigEndian.PutUint64(buf[n:], incarnation)
	n += 8
	copy(buf[n:], slot.Bytes())
	return buf
}

// plainStorageKeyPrefix = PlainStoragePrefix + address + incarnation
func plainStorageKeyPrefix(address common.Address, incarnation uint64) []byte {
	buf := make([]byte, len(PlainStoragePrefix)+common.AddressLength+8)
	n := copy(buf, PlainStoragePrefix)
	n += copy(buf[n:], address.Bytes())
	binary.BigEndian.PutUint64(buf[n:], incarnation)
	return buf
}

// hashedAccountKey = HashedAccountPrefix + account hash
func hashedAccountKey(accountHash common.Hash) []byte {
	return append(HashedAccountPrefix, accountHash.Bytes()...)
}

// hashedStorageKey = HashedStoragePrefix + account hash + incarnation + storage hash
func hashedStorageKey(accountHash common.Hash, incarnation uint64, storageHash common.Hash) []byte {
	buf := make([]byte, len(HashedStoragePrefix)+common.HashLength+8+common.HashLength)
	n := copy(buf, HashedStoragePrefix)
	n += copy(buf[n:], accountHash.Bytes())
	binary.BigEndian.PutUint64(buf[n:], incarnation)
	n += 8
	copy(buf[n:], storageHash.Bytes())
	return buf
}

// hashedStorageKeyPrefix = HashedStoragePrefix + account hash + incarnation
func hashedStorageKeyPrefix(accountHash common.Hash, incarnation uint64) []byte {
	buf := make([]byte, len(HashedStoragePrefix)+common.HashLength+8)
	n := copy(buf, HashedStoragePrefix)
	n += copy(buf[n:], accountHash.Bytes())
	binary.BigEndian.PutUint64(buf[n:], incarnation)
	return buf
}

func PlainAccountKey(address common.Address) []byte {
	return plainAccountKey(address)
}

func PlainStorageKey(address common.Address, incarnation uint64, slot common.Hash) []byte {
	return plainStorageKey(address, incarnation, slot)
}

func HashedAccountKey(accountHash common.Hash) []byte {
	return hashedAccountKey(accountHash)
}

func HashedStorageKey(accountHash common.Hash, incarnation uint64, storageHash common.Hash) []byte {
	return hashedStorageKey(accountHash, incarnation, storageHash)
}

func PlainIncarnationKey(address common.Address) []byte {
	return flatStateIncarnationKeyByAddress(address)
}

func HashedIncarnationKey(accountHash common.Hash) []byte {
	return flatStateIncarnationKeyByHash(accountHash)
}

// flatStateMetaKey = FlatStateMetaPrefix + key
func flatStateMetaKey(key []byte) []byte {
	return append(FlatStateMetaPrefix, key...)
}

func flatDeltaMarkerKey(number uint64, hash common.Hash) []byte {
	key := make([]byte, 0, len("delta_")+8+common.HashLength)
	key = append(key, []byte("delta_")...)
	key = append(key, encodeBlockNumber(number)...)
	key = append(key, hash.Bytes()...)
	return flatStateMetaKey(key)
}

func flatDeltaMarkerPrefix(number uint64) []byte {
	key := make([]byte, 0, len(FlatStateMetaPrefix)+len("delta_")+8)
	key = append(key, FlatStateMetaPrefix...)
	key = append(key, []byte("delta_")...)
	key = append(key, encodeBlockNumber(number)...)
	return key
}

func flatDeltaKey(number uint64, hash common.Hash, idx uint32) []byte {
	buf := make([]byte, len(FlatDeltaPrefix)+8+common.HashLength+4)
	n := copy(buf, FlatDeltaPrefix)
	binary.BigEndian.PutUint64(buf[n:], number)
	n += 8
	n += copy(buf[n:], hash.Bytes())
	binary.BigEndian.PutUint32(buf[n:], idx)
	return buf
}

func flatDeltaKeyPrefix(number uint64, hash common.Hash) []byte {
	buf := make([]byte, len(FlatDeltaPrefix)+8+common.HashLength)
	n := copy(buf, FlatDeltaPrefix)
	binary.BigEndian.PutUint64(buf[n:], number)
	n += 8
	copy(buf[n:], hash.Bytes())
	return buf
}

type FlatDeltaEntry struct {
	Key        []byte
	OldValue   []byte
	OldExisted bool
}

// flatStateIncarnationKeyByAddress = FlatStateMetaPrefix + "inc_a_" + address
func flatStateIncarnationKeyByAddress(address common.Address) []byte {
	key := make([]byte, 0, len("inc_a_")+common.AddressLength)
	key = append(key, []byte("inc_a_")...)
	key = append(key, address.Bytes()...)
	return flatStateMetaKey(key)
}

// flatStateIncarnationKeyByHash = FlatStateMetaPrefix + "inc_h_" + account hash
func flatStateIncarnationKeyByHash(accountHash common.Hash) []byte {
	key := make([]byte, 0, len("inc_h_")+common.HashLength)
	key = append(key, []byte("inc_h_")...)
	key = append(key, accountHash.Bytes()...)
	return flatStateMetaKey(key)
}

// ReadPlainAccount retrieves the plain-KV account entry (RLP(StateAccount)).
func ReadPlainAccount(db ethdb.KeyValueReader, address common.Address) []byte {
	data, _ := db.Get(plainAccountKey(address))
	return data
}

// WritePlainAccount stores the plain-KV account entry (RLP(StateAccount)).
func WritePlainAccount(db ethdb.KeyValueWriter, address common.Address, entry []byte) {
	if err := db.Put(plainAccountKey(address), entry); err != nil {
		log.Crit("Failed to store plain account", "err", err)
	}
}

// DeletePlainAccount removes the plain-KV account entry.
func DeletePlainAccount(db ethdb.KeyValueWriter, address common.Address) {
	if err := db.Delete(plainAccountKey(address)); err != nil {
		log.Crit("Failed to delete plain account", "err", err)
	}
}

// ReadPlainStorage retrieves the plain-KV storage entry.
func ReadPlainStorage(db ethdb.KeyValueReader, address common.Address, incarnation uint64, slot common.Hash) []byte {
	data, _ := db.Get(plainStorageKey(address, incarnation, slot))
	return data
}

// WritePlainStorage stores the plain-KV storage entry.
func WritePlainStorage(db ethdb.KeyValueWriter, address common.Address, incarnation uint64, slot common.Hash, entry []byte) {
	if err := db.Put(plainStorageKey(address, incarnation, slot), entry); err != nil {
		log.Crit("Failed to store plain storage", "err", err)
	}
}

// DeletePlainStorage removes the plain-KV storage entry.
func DeletePlainStorage(db ethdb.KeyValueWriter, address common.Address, incarnation uint64, slot common.Hash) {
	if err := db.Delete(plainStorageKey(address, incarnation, slot)); err != nil {
		log.Crit("Failed to delete plain storage", "err", err)
	}
}

// IteratePlainAccounts returns an iterator for walking all plain-KV accounts.
func IteratePlainAccounts(db ethdb.Iteratee) ethdb.Iterator {
	return NewKeyLengthIterator(db.NewIterator(PlainAccountPrefix, nil), len(PlainAccountPrefix)+common.AddressLength)
}

// IteratePlainStorage returns an iterator for walking the storage of a specific account.
func IteratePlainStorage(db ethdb.Iteratee, address common.Address, incarnation uint64) ethdb.Iterator {
	return NewKeyLengthIterator(db.NewIterator(plainStorageKeyPrefix(address, incarnation), nil), len(PlainStoragePrefix)+common.AddressLength+8+common.HashLength)
}

// ReadHashedAccount retrieves the hashed-KV account entry (RLP(StateAccount)).
func ReadHashedAccount(db ethdb.KeyValueReader, accountHash common.Hash) []byte {
	data, _ := db.Get(hashedAccountKey(accountHash))
	return data
}

// WriteHashedAccount stores the hashed-KV account entry (RLP(StateAccount)).
func WriteHashedAccount(db ethdb.KeyValueWriter, accountHash common.Hash, entry []byte) {
	if err := db.Put(hashedAccountKey(accountHash), entry); err != nil {
		log.Crit("Failed to store hashed account", "err", err)
	}
}

// DeleteHashedAccount removes the hashed-KV account entry.
func DeleteHashedAccount(db ethdb.KeyValueWriter, accountHash common.Hash) {
	if err := db.Delete(hashedAccountKey(accountHash)); err != nil {
		log.Crit("Failed to delete hashed account", "err", err)
	}
}

// ReadHashedStorage retrieves the hashed-KV storage entry.
func ReadHashedStorage(db ethdb.KeyValueReader, accountHash common.Hash, incarnation uint64, storageHash common.Hash) []byte {
	data, _ := db.Get(hashedStorageKey(accountHash, incarnation, storageHash))
	return data
}

// WriteHashedStorage stores the hashed-KV storage entry.
func WriteHashedStorage(db ethdb.KeyValueWriter, accountHash common.Hash, incarnation uint64, storageHash common.Hash, entry []byte) {
	if err := db.Put(hashedStorageKey(accountHash, incarnation, storageHash), entry); err != nil {
		log.Crit("Failed to store hashed storage", "err", err)
	}
}

// DeleteHashedStorage removes the hashed-KV storage entry.
func DeleteHashedStorage(db ethdb.KeyValueWriter, accountHash common.Hash, incarnation uint64, storageHash common.Hash) {
	if err := db.Delete(hashedStorageKey(accountHash, incarnation, storageHash)); err != nil {
		log.Crit("Failed to delete hashed storage", "err", err)
	}
}

// IterateHashedAccounts returns an iterator for walking all hashed-KV accounts.
func IterateHashedAccounts(db ethdb.Iteratee) ethdb.Iterator {
	return NewKeyLengthIterator(db.NewIterator(HashedAccountPrefix, nil), len(HashedAccountPrefix)+common.HashLength)
}

// IterateHashedStorage returns an iterator for walking the storage of a specific account.
func IterateHashedStorage(db ethdb.Iteratee, accountHash common.Hash, incarnation uint64) ethdb.Iterator {
	return NewKeyLengthIterator(db.NewIterator(hashedStorageKeyPrefix(accountHash, incarnation), nil), len(HashedStoragePrefix)+common.HashLength+8+common.HashLength)
}

// ReadFlatStateMeta retrieves flat state metadata (e.g. pivot root, flags).
// Deprecated: use hashed/plain metadata helpers once introduced.
func ReadFlatStateMeta(db ethdb.KeyValueReader, key []byte) []byte {
	data, _ := db.Get(flatStateMetaKey(key))
	return data
}

// WriteFlatStateMeta stores flat state metadata (e.g. pivot root, flags).
// Deprecated: use hashed/plain metadata helpers once introduced.
func WriteFlatStateMeta(db ethdb.KeyValueWriter, key, value []byte) {
	if err := db.Put(flatStateMetaKey(key), value); err != nil {
		log.Crit("Failed to store flat state metadata", "err", err)
	}
}

// DeleteFlatStateMeta removes flat state metadata.
// Deprecated: use hashed/plain metadata helpers once introduced.
func DeleteFlatStateMeta(db ethdb.KeyValueWriter, key []byte) {
	if err := db.Delete(flatStateMetaKey(key)); err != nil {
		log.Crit("Failed to delete flat state metadata", "err", err)
	}
}

func WriteFlatDelta(db ethdb.KeyValueWriter, number uint64, hash common.Hash, idx uint32, entry FlatDeltaEntry) {
	blob, err := rlp.EncodeToBytes(entry)
	if err != nil {
		log.Crit("Failed to encode flat delta entry", "err", err)
	}
	if err := db.Put(flatDeltaKey(number, hash, idx), blob); err != nil {
		log.Crit("Failed to store flat delta entry", "err", err)
	}
}

func WriteFlatDeltaBatch(db ethdb.KeyValueWriter, number uint64, hash common.Hash, entries []FlatDeltaEntry) {
	if err := db.Put(flatDeltaMarkerKey(number, hash), []byte{1}); err != nil {
		log.Crit("Failed to store flat delta marker", "err", err)
	}
	for i, entry := range entries {
		WriteFlatDelta(db, number, hash, uint32(i), entry)
	}
}

func HasFlatDeltaMarker(db ethdb.KeyValueReader, number uint64, hash common.Hash) bool {
	ok, _ := db.Has(flatDeltaMarkerKey(number, hash))
	return ok
}

func ReadFlatDeltaEntries(db ethdb.Iteratee, number uint64, hash common.Hash) ([]FlatDeltaEntry, error) {
	it := db.NewIterator(flatDeltaKeyPrefix(number, hash), nil)
	defer it.Release()

	var entries []FlatDeltaEntry
	for it.Next() {
		var entry FlatDeltaEntry
		if err := rlp.DecodeBytes(it.Value(), &entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := it.Error(); err != nil {
		return nil, err
	}
	return entries, nil
}

func DeleteFlatDeltaRange(db ethdb.KeyValueStore, number uint64) {
	it := db.NewIterator(append(FlatDeltaPrefix, encodeBlockNumber(number)...), nil)
	for it.Next() {
		if err := db.Delete(it.Key()); err != nil {
			log.Crit("Failed to delete flat delta entry", "err", err)
		}
	}
	it.Release()
	if err := it.Error(); err != nil {
		log.Crit("Failed to iterate flat delta entries", "err", err)
	}

	it = db.NewIterator(flatDeltaMarkerPrefix(number), nil)
	for it.Next() {
		if err := db.Delete(it.Key()); err != nil {
			log.Crit("Failed to delete flat delta marker", "err", err)
		}
	}
	it.Release()
	if err := it.Error(); err != nil {
		log.Crit("Failed to iterate flat delta markers", "err", err)
	}
}

// ReadFlatCode retrieves contract code using the shared code store.
// Deprecated: use ReadCode/WriteCode directly or add a hashed/plain wrapper if needed.
func ReadFlatCode(db ethdb.KeyValueReader, codeHash common.Hash) []byte {
	return ReadCode(db, codeHash)
}

// WriteFlatCode stores contract code using the shared code store.
// Deprecated: use ReadCode/WriteCode directly or add a hashed/plain wrapper if needed.
func WriteFlatCode(db ethdb.KeyValueWriter, codeHash common.Hash, code []byte) {
	WriteCode(db, codeHash, code)
}

// ReadPlainIncarnation retrieves the plain-KV incarnation for an account.
func ReadPlainIncarnation(db ethdb.KeyValueReader, address common.Address) (uint64, bool) {
	data, _ := db.Get(flatStateIncarnationKeyByAddress(address))
	if len(data) == 0 {
		return 0, false
	}
	if len(data) != 8 {
		log.Error("Invalid plain incarnation length", "len", len(data))
		return 0, false
	}
	return binary.BigEndian.Uint64(data), true
}

// WritePlainIncarnation stores the plain-KV incarnation for an account.
func WritePlainIncarnation(db ethdb.KeyValueWriter, address common.Address, incarnation uint64) {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, incarnation)
	if err := db.Put(flatStateIncarnationKeyByAddress(address), buf); err != nil {
		log.Crit("Failed to store plain incarnation", "err", err)
	}
}

// ReadHashedIncarnation retrieves the hashed-KV incarnation for an account.
func ReadHashedIncarnation(db ethdb.KeyValueReader, accountHash common.Hash) (uint64, bool) {
	data, _ := db.Get(flatStateIncarnationKeyByHash(accountHash))
	if len(data) == 0 {
		return 0, false
	}
	if len(data) != 8 {
		log.Error("Invalid hashed incarnation length", "len", len(data))
		return 0, false
	}
	return binary.BigEndian.Uint64(data), true
}

// WriteHashedIncarnation stores the hashed-KV incarnation for an account.
func WriteHashedIncarnation(db ethdb.KeyValueWriter, accountHash common.Hash, incarnation uint64) {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, incarnation)
	if err := db.Put(flatStateIncarnationKeyByHash(accountHash), buf); err != nil {
		log.Crit("Failed to store hashed incarnation", "err", err)
	}
}
