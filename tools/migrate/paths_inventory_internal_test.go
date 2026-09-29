package migrate

import (
	"io"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countedInventoryDirectory struct {
	remaining      int
	read           int
	largestRequest int
}

func (d *countedInventoryDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
	d.largestRequest = max(d.largestRequest, count)
	if d.remaining == 0 {
		return nil, io.EOF
	}
	if count <= 0 {
		return nil, io.ErrUnexpectedEOF
	}
	size := min(count, d.remaining)
	d.remaining -= size
	d.read += size
	return make([]fs.DirEntry, size), nil
}

func TestInventoryStopsReadingAtSharedBudget(t *testing.T) {
	t.Parallel()
	const budget = 65
	remaining := budget
	directory := &countedInventoryDirectory{remaining: 1000000}
	err := readInventoryEntries(directory, &remaining, func(fs.DirEntry) error { return nil })
	require.EqualError(t, err, "snapshot_entry_limit_exceeded")
	assert.Equal(t, budget+1, directory.read, "oversized directory must not be enumerated in full")
	assert.LessOrEqual(t, directory.largestRequest, inventoryReadBatch)
	assert.Positive(t, directory.remaining)
}

func TestInventoryReservesParentBatchBeforeDescending(t *testing.T) {
	t.Parallel()
	remaining := 4
	parent := &countedInventoryDirectory{remaining: 2}
	child := &countedInventoryDirectory{remaining: 1000000}
	err := readInventoryEntries(parent, &remaining, func(fs.DirEntry) error {
		assert.Equal(t, 2, remaining, "prefetched parent entries must already consume budget")
		return readInventoryEntries(child, &remaining, func(fs.DirEntry) error { return nil })
	})
	require.EqualError(t, err, "snapshot_entry_limit_exceeded")
	assert.Equal(t, 5, parent.read+child.read)
}

func TestInventoryAcceptsExactlyFullBudget(t *testing.T) {
	t.Parallel()
	remaining := inventoryReadBatch
	directory := &countedInventoryDirectory{remaining: inventoryReadBatch}
	err := readInventoryEntries(directory, &remaining, func(fs.DirEntry) error { return nil })
	require.NoError(t, err)
	assert.Zero(t, remaining)
	assert.Equal(t, inventoryReadBatch, directory.read)
}
