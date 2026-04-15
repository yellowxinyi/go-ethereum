package state

import (
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/triedb"
)

func TestObservationModeRejectsNoStorageWipingFalse(t *testing.T) {
	disk := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(disk, nil)
	db := NewDatabase(tdb, nil)
	db.EnableObservationMode(disk)

	st, err := New(types.EmptyRootHash, db)
	if err != nil {
		t.Fatalf("create state: %v", err)
	}
	_, _, err = st.CommitWithUpdate(1, true, false)
	if err == nil {
		t.Fatal("expected error for observation mode with noStorageWiping=false")
	}
	if !strings.Contains(err.Error(), "observation mode requires Cancun+ semantics (noStorageWiping=true)") {
		t.Fatalf("unexpected error: %v", err)
	}
}

