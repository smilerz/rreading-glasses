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

func TestGuessLanguage(t *testing.T) {
	t.Parallel()

	foreign := map[string]string{
		"Нужные вещи": "rus",
		"Противостояние. Том 1. 16 июня - 4 июля 1990": "rus",
		"Страшни сънища за продан 2":                   "rus",
		"Το κοράκι":                                  "gre",
		"Der Fall des Doktors":                       "ger",
		"Stephen Kings Der dunkle Turm und das Ende": "ger",
		"Les petites sœurs d'Elurie":                 "fre",
		"Das Buch der Geister & Spukhäuser":          "ger",
		"Fyra Skräcknoveller och andra":              "swe",
	}
	for title, want := range foreign {
		assert.Equal(t, want, guessLanguage(title), title)
	}

	// English or undecidable titles must stay unlabeled.
	unlabeled := []string{
		"Needful Things", "Mr. Mercedes", "Mr. Mercedes: A Novel", "It", "Misery",
		"Cell", "The Stand", "Pet Sematary", "Doctor Sleep", "11/22/63",
		"Different Seasons: Volume 2 - Fall/Winter", "The Dark Tower: Books 1-2 Omnibus",
		"The Shining: الكتاب الأول 1927-1940", "The Stand, Part I: Том 1",
		"The Man in the Black Suit", "El Deafo", "Cep", "Memoria", "Köplust", "La tienda",
		"Fairy Tale", "", "1408",
		// A single function word is not enough evidence (Hardcover tags this edition "ger" anyway).
		"Stephen Kings Der dunkle Turm, Band 14",
	}
	for _, title := range unlabeled {
		assert.Empty(t, guessLanguage(title), title)
	}
}

func TestLanguageOrGuess(t *testing.T) {
	// Not parallel: toggles package state.
	t.Cleanup(func() { SetInferLanguage(false) })

	SetInferLanguage(false)
	assert.Equal(t, "", languageOrGuess("", "Der Fall des Doktors"), "off by default")

	SetInferLanguage(true)
	assert.Equal(t, "ger", languageOrGuess("", "Der Fall des Doktors"))
	assert.Equal(t, "eng", languageOrGuess("eng", "Der Fall des Doktors"), "explicit language wins")
	assert.Equal(t, "", languageOrGuess("", "Needful Things"))
}

func TestDenormalizeWorksInfersLanguage(t *testing.T) {
	// Not parallel: toggles package state.
	t.Cleanup(func() { SetInferLanguage(false) })
	ctx := context.Background()

	authorID := int64(1000)
	work := workResource{
		ForeignID: 1,
		Title:     "Work",
		Authors:   []AuthorResource{{ForeignID: authorID}},
		Books: []bookResource{
			{ForeignID: 10, Title: "Needful Things"},
			{ForeignID: 11, Title: "Нужные вещи"},
			{ForeignID: 12, Title: "Der Fall des Doktors"},
			{ForeignID: 13, Title: "Der Fall des Doktors", Language: "eng"},
		},
	}
	workBytes, err := json.Marshal(work)
	require.NoError(t, err)

	getter := NewMockgetter(gomock.NewController(t))
	getter.EXPECT().GetWork(gomock.Any(), work.ForeignID, gomock.Any()).Return(workBytes, authorID, nil).AnyTimes()

	snapshot, err := json.Marshal(AuthorResource{ForeignID: authorID})
	require.NoError(t, err)
	cache := newMemoryCache()
	cache.Set(ctx, AuthorKey(authorID), snapshot, time.Hour)

	ctrl, err := NewController(cache, getter, nil, nil)
	require.NoError(t, err)

	languages := func() map[int64]string {
		cache.Set(ctx, AuthorKey(authorID), snapshot, time.Hour) // Start from the unmerged author each time.
		require.NoError(t, ctrl.denormalizeWorks(ctx, authorID, work.ForeignID))
		authorBytes, ok := cache.Get(ctx, AuthorKey(authorID))
		require.True(t, ok)
		var author AuthorResource
		require.NoError(t, json.Unmarshal(authorBytes, &author))
		require.Len(t, author.Works, 1)
		out := map[int64]string{}
		for _, b := range author.Works[0].Books {
			out[b.ForeignID] = b.Language
		}
		return out
	}

	SetInferLanguage(false)
	assert.Equal(t, map[int64]string{10: "", 11: "", 12: "", 13: "eng"}, languages(), "off by default")

	SetInferLanguage(true)
	assert.Equal(t, map[int64]string{10: "", 11: "rus", 12: "ger", 13: "eng"}, languages())
}
