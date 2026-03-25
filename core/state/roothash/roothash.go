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

package roothash

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

// ComputeHashedStateRoot calculates the state root using the hashed KV view.
// It reconstructs the account/storage tries in a streaming fashion without
// persisting trie nodes.
func ComputeHashedStateRoot(db ethdb.KeyValueStore) (common.Hash, error) {
	iter := rawdb.IterateHashedAccounts(db)
	defer iter.Release()

	stack := trie.NewStackTrie(nil)
	var accounts int
	for iter.Next() {
		key := iter.Key()
		if len(key) != len(rawdb.HashedAccountPrefix)+common.HashLength {
			return common.Hash{}, fmt.Errorf("invalid hashed account key length: %d", len(key))
		}
		accountHash := key[len(rawdb.HashedAccountPrefix):]
		value := iter.Value()
		if len(value) == 0 {
			continue // deletion marker, skip
		}
		var account types.StateAccount
		if err := rlp.DecodeBytes(value, &account); err != nil {
			return common.Hash{}, err
		}
		if len(account.CodeHash) == 0 {
			account.CodeHash = types.EmptyCodeHash.Bytes()
		}
		incarnation, _ := rawdb.ReadHashedIncarnation(db, common.BytesToHash(accountHash))
		root, err := computeHashedStorageRoot(db, common.BytesToHash(accountHash), incarnation)
		if err != nil {
			return common.Hash{}, err
		}
		account.Root = root

		enc, err := rlp.EncodeToBytes(&account)
		if err != nil {
			return common.Hash{}, err
		}
		if err := stack.Update(accountHash, enc); err != nil {
			return common.Hash{}, err
		}
		accounts++
	}
	if err := iter.Error(); err != nil {
		return common.Hash{}, err
	}
	if accounts == 0 {
		return types.EmptyRootHash, nil
	}
	return stack.Hash(), nil
}

func computeHashedStorageRoot(db ethdb.KeyValueStore, accountHash common.Hash, incarnation uint64) (common.Hash, error) {
	iter := rawdb.IterateHashedStorage(db, accountHash, incarnation)
	defer iter.Release()

	stack := trie.NewStackTrie(nil)
	var slots int
	for iter.Next() {
		key := iter.Key()
		if len(key) != len(rawdb.HashedStoragePrefix)+common.HashLength+8+common.HashLength {
			return common.Hash{}, fmt.Errorf("invalid hashed storage key length: %d", len(key))
		}
		slotHash := key[len(rawdb.HashedStoragePrefix)+common.HashLength+8:]
		value := iter.Value()
		if len(value) == 0 {
			continue // deletion marker, skip
		}
		if err := stack.Update(slotHash, value); err != nil {
			return common.Hash{}, err
		}
		slots++
	}
	if err := iter.Error(); err != nil {
		return common.Hash{}, err
	}
	if slots == 0 {
		return types.EmptyRootHash, nil
	}
	return stack.Hash(), nil
}
