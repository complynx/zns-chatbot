package integration_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/stickercache"
)

func stickerKey(id string) stickercache.Key {
	return stickercache.Key{Kind: stickercache.Sticker, AssetID: id, DescriptorVersion: "canonical-v1"}
}

func TestStickerCacheConcurrentMissRestartAndVersion(t *testing.T) {
	t.Parallel()
	db := database(t)
	cache, err := stickercache.New(db, stickercache.Options{})
	require.NoError(t, err)
	other, err := stickercache.New(db, stickercache.Options{})
	require.NoError(t, err)
	var calls atomic.Int32
	process := func(ctx context.Context, _ stickercache.Key) (string, error) {
		calls.Add(1)
		return "A red fox waves its paw.", ctx.Err()
	}
	const workers = 8
	var group sync.WaitGroup
	results := make(chan error, workers)
	for index := range workers {
		group.Go(func() {
			selected := cache
			if index%2 == 0 {
				selected = other
			}
			description, getErr := selected.Get(t.Context(), stickerKey("fox"), process)
			if getErr == nil && description != "A red fox waves its paw." {
				getErr = errors.New("unexpected description")
			}
			results <- getErr
		})
	}
	group.Wait()
	close(results)
	for result := range results {
		require.NoError(t, result)
	}
	assert.EqualValues(t, 1, calls.Load())
	restarted, err := stickercache.New(db, stickercache.Options{})
	require.NoError(t, err)
	_, err = restarted.Get(t.Context(), stickerKey("fox"), nil)
	require.NoError(t, err)
	versioned := stickerKey("fox")
	versioned.DescriptorVersion = "canonical-v2"
	_, err = restarted.Get(t.Context(), versioned, process)
	require.NoError(t, err)
	assert.EqualValues(t, 2, calls.Load())
	var count int
	require.NoError(
		t,
		db.QueryRow(t.Context(), "SELECT count(*) FROM bot.sticker_descriptions WHERE description IS NOT NULL").
			Scan(&count),
	)
	assert.Equal(t, 2, count)
	versioned.Kind = stickercache.CustomEmoji
	_, err = restarted.Get(t.Context(), versioned, process)
	require.NoError(t, err)
	assert.EqualValues(t, 3, calls.Load(), "asset kinds have separate identities")
}

func TestStickerCacheDecayAdmissionAndMetadata(t *testing.T) {
	t.Parallel()
	db := database(t)
	now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	cache, err := stickercache.New(
		db,
		stickercache.Options{Capacity: 2, HalfLife: time.Hour, Now: func() time.Time { return now }},
	)
	require.NoError(t, err)
	process := func(_ context.Context, key stickercache.Key) (string, error) {
		return "Canonical asset " + key.AssetID, nil
	}
	for range 8 {
		_, err = cache.Get(t.Context(), stickerKey("old"), process)
		require.NoError(t, err)
	}
	now = now.Add(4 * time.Hour)
	_, err = cache.Get(t.Context(), stickerKey("recent"), process)
	require.NoError(t, err)
	_, err = cache.Get(t.Context(), stickerKey("new"), process)
	require.NoError(t, err)
	var description *string
	var score float64
	require.NoError(
		t,
		db.QueryRow(t.Context(), "SELECT description,score FROM bot.sticker_descriptions WHERE asset_id='old'").
			Scan(&description, &score),
	)
	assert.Nil(t, description)
	assert.InDelta(t, 8, score, 0.00001, "eviction computes lazy decay without rewriting stored score")
	_, err = cache.Get(t.Context(), stickerKey("old"), process)
	require.NoError(t, err)
	require.NoError(
		t,
		db.QueryRow(t.Context(), "SELECT score FROM bot.sticker_descriptions WHERE asset_id='old'").Scan(&score),
	)
	assert.InDelta(t, 3.5, score, 0.00001)
	var residents, metadata int
	require.NoError(
		t,
		db.QueryRow(t.Context(), "SELECT count(description),count(*) FROM bot.sticker_descriptions").
			Scan(&residents, &metadata),
	)
	assert.Equal(t, 2, residents)
	assert.Equal(t, 3, metadata)
}

func TestStickerCacheFailureRetryAndLimits(t *testing.T) {
	t.Parallel()
	db := database(t)
	cache, err := stickercache.New(db, stickercache.Options{Capacity: 1})
	require.NoError(t, err)
	failure := errors.New("processor unavailable")
	_, err = cache.Get(
		t.Context(),
		stickerKey("bad"),
		func(context.Context, stickercache.Key) (string, error) { return "", failure },
	)
	require.ErrorIs(t, err, failure)
	_, err = cache.Get(t.Context(), stickerKey("bad"), func(context.Context, stickercache.Key) (string, error) {
		return strings.Repeat("я", stickercache.MaxDescriptionBytes), nil
	})
	require.ErrorIs(t, err, stickercache.ErrDescription)
	_, err = cache.Get(
		t.Context(),
		stickerKey("bad"),
		func(context.Context, stickercache.Key) (string, error) { return "A smiling face.", nil },
	)
	require.NoError(t, err)
	_, err = cache.Get(t.Context(), stickerKey("bad"), nil)
	require.NoError(t, err)
	_, err = cache.Get(
		t.Context(),
		stickerKey("replacement"),
		func(context.Context, stickercache.Key) (string, error) { return "A blue heart.", nil },
	)
	require.NoError(t, err)
	var residents int
	require.NoError(
		t,
		db.QueryRow(t.Context(), "SELECT count(description) FROM bot.sticker_descriptions").Scan(&residents),
	)
	assert.Equal(t, 1, residents)
	_, err = stickercache.New(db, stickercache.Options{Capacity: -1})
	require.ErrorIs(t, err, stickercache.ErrConfiguration)
	_, err = stickercache.New(db, stickercache.Options{HalfLife: -time.Second})
	require.ErrorIs(t, err, stickercache.ErrConfiguration)
	_, err = cache.Get(t.Context(), stickerKey(""), nil)
	require.ErrorIs(t, err, stickercache.ErrKey)
}

func TestStickerCacheAdmissionFirstHalf(t *testing.T) {
	t.Parallel()
	for _, capacity := range []int{4, 5, 8} {
		t.Run(strconv.Itoa(capacity), func(t *testing.T) {
			t.Parallel()
			db := database(t)
			now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
			cache, err := stickercache.New(db, stickercache.Options{
				Capacity: capacity, HalfLife: time.Hour, Now: func() time.Time { return now },
			})
			require.NoError(t, err)
			_, err = db.Exec(t.Context(), `INSERT INTO bot.sticker_descriptions
				(kind,asset_id,descriptor_version,score,scored_at,description)
				SELECT 'sticker','existing-'||i,'canonical-v1',i*10,$1,'Existing asset'
				FROM generate_series(1,5) AS i`, now)
			require.NoError(t, err)
			_, err = cache.Get(t.Context(), stickerKey("new"), func(context.Context, stickercache.Key) (string, error) {
				return "A new animated asset.", nil
			})
			require.NoError(t, err)
			var rank, residents int
			require.NoError(t, db.QueryRow(t.Context(), `SELECT
				1+count(*) FILTER (WHERE score>(SELECT score FROM bot.sticker_descriptions WHERE asset_id='new')),
				count(*) FROM bot.sticker_descriptions WHERE description IS NOT NULL`).Scan(&rank, &residents))
			assert.LessOrEqual(t, residents, capacity)
			assert.LessOrEqual(t, rank, (residents+1)/2)
			// Evicted metadata retains popularity above the admission threshold.
			_, err = db.Exec(t.Context(), `INSERT INTO bot.sticker_descriptions
				(kind,asset_id,descriptor_version,score,scored_at)
				VALUES('sticker','returning','canonical-v1',1000,$1)`, now)
			require.NoError(t, err)
			_, err = cache.Get(
				t.Context(),
				stickerKey("returning"),
				func(context.Context, stickercache.Key) (string, error) {
					return "A returning asset.", nil
				},
			)
			require.NoError(t, err)
			var score float64
			require.NoError(
				t,
				db.QueryRow(t.Context(), "SELECT score FROM bot.sticker_descriptions WHERE asset_id='returning'").
					Scan(&score),
			)
			assert.InDelta(t, 1001, score, 0.00001)
		})
	}
}

func TestStickerCacheCancelledProcessorReleasesLock(t *testing.T) {
	t.Parallel()
	db := database(t)
	cache, err := stickercache.New(db, stickercache.Options{})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_, err = cache.Get(ctx, stickerKey("cancelled"), func(ctx context.Context, _ stickercache.Key) (string, error) {
		cancel()
		return "", ctx.Err()
	})
	require.ErrorIs(t, err, context.Canceled)
	retryCtx, retryCancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer retryCancel()
	_, err = cache.Get(retryCtx, stickerKey("cancelled"), func(context.Context, stickercache.Key) (string, error) {
		return strings.Repeat("a", stickercache.MaxDescriptionBytes), nil
	})
	require.NoError(t, err)
	var locks int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM pg_locks
		WHERE locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database())`).Scan(&locks))
	assert.Zero(t, locks)
}

func TestStickerCacheConcurrentAdmissionBound(t *testing.T) {
	t.Parallel()
	db := database(t)
	cache, err := stickercache.New(db, stickercache.Options{Capacity: 2})
	require.NoError(t, err)
	const workers = 12
	results := make(chan error, workers)
	var group sync.WaitGroup
	for index := range workers {
		group.Go(func() {
			_, getErr := cache.Get(t.Context(), stickerKey(strconv.Itoa(index)),
				func(ctx context.Context, _ stickercache.Key) (string, error) { return "An asset.", ctx.Err() })
			results <- getErr
		})
	}
	group.Wait()
	close(results)
	for result := range results {
		require.NoError(t, result)
	}
	var residents, metadata int
	require.NoError(
		t,
		db.QueryRow(t.Context(), "SELECT count(description),count(*) FROM bot.sticker_descriptions").
			Scan(&residents, &metadata),
	)
	assert.Equal(t, 2, residents)
	assert.Equal(t, workers, metadata)
}

func TestStickerCacheLongIdleDecay(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"hit", "admission", "slow_processor"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			db := database(t)
			now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
			cache, err := stickercache.New(db, stickercache.Options{
				Capacity: 2, HalfLife: time.Second, Now: func() time.Time { return now },
			})
			require.NoError(t, err)
			process := func(ctx context.Context, _ stickercache.Key) (string, error) {
				return "A canonical asset.", ctx.Err()
			}
			for _, id := range []string{"first", "second"} {
				_, err = cache.Get(t.Context(), stickerKey(id), process)
				require.NoError(t, err)
			}
			if operation == "slow_processor" {
				_, err = cache.Get(
					t.Context(),
					stickerKey("new"),
					func(ctx context.Context, _ stickercache.Key) (string, error) {
						now = now.Add(1100 * time.Second)
						return "A slowly described asset.", ctx.Err()
					},
				)
			} else {
				now = now.Add(1100 * time.Second)
				id := "new"
				if operation == "hit" {
					id = "first"
					process = nil
				}
				_, err = cache.Get(t.Context(), stickerKey(id), process)
			}
			require.NoError(t, err)
			var residents int
			require.NoError(
				t,
				db.QueryRow(t.Context(), "SELECT count(description) FROM bot.sticker_descriptions").Scan(&residents),
			)
			assert.Equal(t, 2, residents)
			var refreshedScore float64
			assetID := "new"
			if operation == "hit" {
				assetID = "first"
			}
			require.NoError(t, db.QueryRow(t.Context(),
				"SELECT score FROM bot.sticker_descriptions WHERE asset_id=$1", assetID).Scan(&refreshedScore))
			assert.InDelta(t, 1, refreshedScore, 0.00001)
		})
	}
}
