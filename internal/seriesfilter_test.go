package internal

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestInIgnoredSeries(t *testing.T) {
	// Not parallel: toggles package state.
	t.Cleanup(func() { _ = SetIgnoreSeries("") })

	inSeries := func(titles ...string) workResource {
		w := workResource{}
		for _, title := range titles {
			w.Series = append(w.Series, SeriesResource{Title: title})
		}
		return w
	}

	require.NoError(t, SetIgnoreSeries(""))
	assert.False(t, inIgnoredSeries(inSeries("The Stand (Split Volumes)")), "off by default")

	require.NoError(t, SetIgnoreSeries(`split[- ]volumes?|\(\d+ volumes?\)`))
	for _, title := range []string{
		"The Stand (Split Volumes)", "The Talisman Split-Volume", "Duma Key Split-Volume",
		"It (2 volumes)", "Nightmares & Dreamscapes: Audiobook Split Volumes", "UNDER THE DOME SPLIT VOLUME",
	} {
		assert.True(t, inIgnoredSeries(inSeries(title)), title)
	}
	for _, title := range []string{
		"The Dark Tower", "Legends", "Different Seasons", "Skeleton Crew", "Four Past Midnight", "",
	} {
		assert.False(t, inIgnoredSeries(inSeries(title)), title)
	}
	assert.False(t, inIgnoredSeries(workResource{}), "no series")
	assert.True(t, inIgnoredSeries(inSeries("The Dark Tower", "It (2 volumes)")), "any series matching is enough")

	assert.Error(t, SetIgnoreSeries("("), "invalid pattern is rejected")
}

func TestDenormalizeWorksSkipsIgnoredSeries(t *testing.T) {
	// Not parallel: toggles package state.
	t.Cleanup(func() { _ = SetIgnoreSeries("") })
	ctx := context.Background()

	authorID := int64(1000)
	mk := func(id int64, series string) []byte {
		w := workResource{
			ForeignID: id, Title: "work",
			Books:   []bookResource{{ForeignID: id * 100, Title: "work"}},
			Authors: []AuthorResource{{ForeignID: authorID}},
		}
		if series != "" {
			w.Series = []SeriesResource{{ForeignID: 5, Title: series, LinkItems: []seriesWorkLinkResource{{ForeignWorkID: id}}}}
		}
		b, err := json.Marshal(w)
		require.NoError(t, err)
		return b
	}

	getter := NewMockgetter(gomock.NewController(t))
	getter.EXPECT().GetWork(gomock.Any(), int64(1), gomock.Any()).Return(mk(1, ""), authorID, nil).AnyTimes()
	getter.EXPECT().GetWork(gomock.Any(), int64(2), gomock.Any()).Return(mk(2, "It (2 volumes)"), authorID, nil).AnyTimes()
	getter.EXPECT().GetSeries(gomock.Any(), int64(5)).Return(&SeriesResource{ForeignID: 5, Title: "It (2 volumes)"}, nil).AnyTimes()

	snapshot, err := json.Marshal(AuthorResource{ForeignID: authorID})
	require.NoError(t, err)
	cache := newMemoryCache()
	ctrl, err := NewController(cache, getter, nil, nil)
	require.NoError(t, err)

	works := func() []int64 {
		authorBytes, ok := cache.Get(ctx, AuthorKey(authorID))
		require.True(t, ok)
		var author AuthorResource
		require.NoError(t, json.Unmarshal(authorBytes, &author))
		ids := []int64{}
		for _, w := range author.Works {
			ids = append(ids, w.ForeignID)
		}
		return ids
	}

	// Off: both works are kept.
	require.NoError(t, SetIgnoreSeries(""))
	cache.Set(ctx, AuthorKey(authorID), snapshot, time.Hour)
	require.NoError(t, ctrl.denormalizeWorks(ctx, authorID, 1, 2))
	assert.Equal(t, []int64{1, 2}, works())

	// On: the split-volume work is dropped, and removed if it was already there.
	require.NoError(t, SetIgnoreSeries(`\(\d+ volumes?\)`))
	require.NoError(t, ctrl.denormalizeWorks(ctx, authorID, 1, 2))
	assert.Equal(t, []int64{1}, works())
}
