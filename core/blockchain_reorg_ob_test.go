package core

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/beacon"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
)

func readPlainBalance(t *testing.T, db ethdb.KeyValueReader, addr common.Address) *uint256.Int {
	data := rawdb.ReadPlainAccount(db, addr)
	if len(data) == 0 {
		return new(uint256.Int)
	}
	var acct types.StateAccount
	if err := rlp.DecodeBytes(data, &acct); err != nil {
		t.Fatalf("decode plain account: %v", err)
	}
	if acct.Balance == nil {
		return new(uint256.Int)
	}
	return acct.Balance
}

func readHashedBalance(t *testing.T, db ethdb.KeyValueReader, addr common.Address) *uint256.Int {
	data := rawdb.ReadHashedAccount(db, crypto.Keccak256Hash(addr.Bytes()))
	if len(data) == 0 {
		return new(uint256.Int)
	}
	var acct types.StateAccount
	if err := rlp.DecodeBytes(data, &acct); err != nil {
		t.Fatalf("decode hashed account: %v", err)
	}
	if acct.Balance == nil {
		return new(uint256.Int)
	}
	return acct.Balance
}

func TestObservationModeReorgFlatDelta(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	from := crypto.PubkeyToAddress(key.PublicKey)
	key2, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key2: %v", err)
	}
	from2 := crypto.PubkeyToAddress(key2.PublicKey)
	fromHash := crypto.Keccak256Hash(from.Bytes())
	chainConfig := *params.TestChainConfig
	shanghai := uint64(0)
	cancun := uint64(0)
	chainConfig.ShanghaiTime = &shanghai
	chainConfig.CancunTime = &cancun
	chainConfig.BlobScheduleConfig = params.DefaultBlobSchedule
	chainConfig.TerminalTotalDifficulty = big.NewInt(0)
	gspec := &Genesis{
		Config: &chainConfig,
		Alloc: GenesisAlloc{
			from:  {Balance: big.NewInt(1_000_000_000_000_000_000)},
			from2: {Balance: big.NewInt(1_000_000_000_000_000_000)},
		},
	}
	genesis := gspec.MustCommit(db, triedb.NewDatabase(db, nil))
	account := types.StateAccount{
		Nonce:    0,
		Balance:  uint256.MustFromBig(big.NewInt(1_000_000_000_000_000_000)),
		Root:     types.EmptyRootHash,
		CodeHash: types.EmptyCodeHash.Bytes(),
	}
	accountRLP, err := rlp.EncodeToBytes(&account)
	if err != nil {
		t.Fatalf("encode genesis account: %v", err)
	}
	rawdb.WritePlainAccount(db, from, accountRLP)
	rawdb.WriteHashedAccount(db, fromHash, accountRLP)
	rawdb.WritePlainIncarnation(db, from, 0)
	rawdb.WriteHashedIncarnation(db, fromHash, 0)
	rawdb.WritePlainAccount(db, from2, accountRLP)
	rawdb.WriteHashedAccount(db, crypto.Keccak256Hash(from2.Bytes()), accountRLP)
	rawdb.WritePlainIncarnation(db, from2, 0)
	rawdb.WriteHashedIncarnation(db, crypto.Keccak256Hash(from2.Bytes()), 0)

	engine := beacon.New(ethash.NewFaker())
	cfg := DefaultConfig()
	cfg.ObservationMode = true
	cfg.SnapBodyKeepBlocks = 128

	chain, err := NewBlockChain(db, gspec, engine, cfg)
	if err != nil {
		t.Fatalf("NewBlockChain: %v", err)
	}
	defer chain.Stop()

	addrA := common.Address{0x22}
	addrB := common.Address{0x33}

	blocksA, _ := GenerateChain(gspec.Config, genesis, engine, db, 3, func(i int, gen *BlockGen) {
		if i > 0 {
			signer := types.MakeSigner(gspec.Config, gen.Number(), gen.Timestamp())
			tx := types.NewTx(&types.LegacyTx{
				Nonce:    uint64(i - 1),
				To:       &addrA,
				Value:    big.NewInt(1),
				Gas:      21000,
				GasPrice: big.NewInt(1_000_000_000),
			})
			signed, err := types.SignTx(tx, signer, key)
			if err != nil {
				t.Fatalf("sign tx A: %v", err)
			}
			gen.AddTx(signed)
		}
	})
	if _, err := chain.InsertChain(blocksA); err != nil {
		t.Fatalf("insert chain A: %v", err)
	}

	plainA := readPlainBalance(t, db, addrA)
	hashedA := readHashedBalance(t, db, addrA)
	if plainA.IsZero() || hashedA.IsZero() {
		t.Fatalf("expected addrA balance > 0 after chain A, plain=%v hashed=%v", plainA, hashedA)
	}

	blocksB, _ := GenerateChain(gspec.Config, blocksA[0], engine, db, 3, func(i int, gen *BlockGen) {
		signer := types.MakeSigner(gspec.Config, gen.Number(), gen.Timestamp())
		tx := types.NewTx(&types.LegacyTx{
			Nonce:    uint64(i),
			To:       &addrB,
			Value:    big.NewInt(1),
			Gas:      21000,
			GasPrice: big.NewInt(1_000_000_000),
		})
		signed, err := types.SignTx(tx, signer, key2)
		if err != nil {
			t.Fatalf("sign tx B: %v", err)
		}
		gen.AddTx(signed)
	})
	if _, err := chain.InsertChain(blocksB); err != nil {
		t.Fatalf("insert chain B: %v", err)
	}

	if head := chain.CurrentBlock(); head.Hash() != blocksB[len(blocksB)-1].Hash() {
		t.Fatalf("expected head to be new fork, have %x want %x", head.Hash(), blocksB[len(blocksB)-1].Hash())
	}

	plainA2 := readPlainBalance(t, db, addrA)
	hashedA2 := readHashedBalance(t, db, addrA)
	if !plainA2.IsZero() || !hashedA2.IsZero() {
		t.Fatalf("expected addrA balance to be reverted, plain=%v hashed=%v", plainA2, hashedA2)
	}

	plainB := readPlainBalance(t, db, addrB)
	hashedB := readHashedBalance(t, db, addrB)
	if plainB.IsZero() || hashedB.IsZero() {
		t.Fatalf("expected addrB balance > 0 after reorg, plain=%v hashed=%v", plainB, hashedB)
	}
}

func TestObservationModeRejectsPreCancunBlock(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	gspec := &Genesis{
		Config: params.TestChainConfig,
	}
	genesis := gspec.MustCommit(db, triedb.NewDatabase(db, nil))
	engine := ethash.NewFaker()
	cfg := DefaultConfig()
	cfg.ObservationMode = true
	cfg.SnapBodyKeepBlocks = 128

	chain, err := NewBlockChain(db, gspec, engine, cfg)
	if err != nil {
		t.Fatalf("NewBlockChain: %v", err)
	}
	defer chain.Stop()

	blocks, _ := GenerateChain(gspec.Config, genesis, engine, db, 1, nil)
	if _, err := chain.InsertChain(blocks); err == nil {
		t.Fatal("expected pre-cancun rejection in observation mode")
	} else if !strings.Contains(err.Error(), "observation mode requires Cancun+ semantics (noStorageWiping=true)") {
		t.Fatalf("unexpected error: %v", err)
	}
}
