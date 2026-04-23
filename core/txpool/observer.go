package txpool

import (
	"bufio"
	"fmt"
	"os"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/pebble"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

// PoolSnapshot captures a reset-time txpool snapshot.
type PoolSnapshot struct {
	BlockNumber uint64
	Pending     map[common.Address][]*types.Transaction
	Queue       map[common.Address][]*types.Transaction
}

// SnapshotObserver asynchronously persists txpool snapshots in a text file and
// a KV database (txhash -> rlp(tx)), with an in-memory LRU dedupe cache.
type SnapshotObserver struct {
	name string
	db   ethdb.KeyValueStore

	ch chan *PoolSnapshot
	wg sync.WaitGroup
}

func NewSnapshotObserver(name, dbPath, hashFile string) (*SnapshotObserver, error) {
	db, err := pebble.New(dbPath, 1024, 1024, "", false)
	if err != nil {
		return nil, fmt.Errorf("open snapshot db (%s): %w", dbPath, err)
	}
	observer := &SnapshotObserver{
		name: name,
		db:   db,
		ch:   make(chan *PoolSnapshot, 64),
	}
	observer.wg.Add(1)
	go observer.worker(hashFile)
	return observer, nil
}

// Observe enqueues a snapshot asynchronously. When the observer queue is full,
// the snapshot is dropped to keep txpool reset path non-blocking.
func (o *SnapshotObserver) Observe(snapshot *PoolSnapshot) {
	if o == nil || snapshot == nil {
		return
	}
	select {
	case o.ch <- snapshot:
	default:
		panic(fmt.Errorf("txpool snapshot channel full, observer=%s block=%d", o.name, snapshot.BlockNumber))
	}
}

func (o *SnapshotObserver) Close() error {
	if o == nil {
		return nil
	}
	close(o.ch)
	o.wg.Wait()
	return o.db.Close()
}

func (o *SnapshotObserver) worker(hashFile string) {
	defer o.wg.Done()

	file, err := os.OpenFile(hashFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	defer writer.Flush()

	cache := lru.NewBasicLRU[common.Hash, struct{}](10000)

	for data := range o.ch {
		log.Info("Snapshot worker processing snapshot", "observer", o.name, "num", data.BlockNumber)
		if _, err := fmt.Fprintln(writer, data.BlockNumber); err != nil {
			panic(err)
		}
		batch := o.db.NewBatch()
		for _, pool := range []map[common.Address][]*types.Transaction{data.Pending, data.Queue} {
			for _, txs := range pool {
				for _, tx := range txs {
					hash := tx.Hash()
					if !cache.Contains(hash) {
						enc, err := rlp.EncodeToBytes(tx)
						if err != nil {
							panic(err)
						}
						if err := batch.Put(hash.Bytes(), enc); err != nil {
							panic(err)
						}
						cache.Add(hash, struct{}{})
					}
					if _, err := fmt.Fprintln(writer, hash); err != nil {
						panic(err)
					}
				}
			}
		}
		if err := batch.Write(); err != nil {
			log.Warn("Failed to persist tx snapshot batch", "observer", o.name, "err", err)
		}
		if err := writer.Flush(); err != nil {
			panic(err)
		}
		log.Info("Snapshot worker finished snapshot", "observer", o.name)
	}
}
