package core

import (
	"fmt"
	"testing"

	"github.com/line/ostracon/types"
	"github.com/stretchr/testify/require"

	"github.com/stretchr/testify/assert"
)

func TestPaginationPage(t *testing.T) {
	cases := []struct {
		totalCount int
		perPage    int
		page       int
		newPage    int
		expErr     bool
	}{
		{0, 10, 1, 1, false},

		{0, 10, 0, 1, false},
		{0, 10, 1, 1, false},
		{0, 10, 2, 0, true},

		{5, 10, -1, 0, true},
		{5, 10, 0, 1, false},
		{5, 10, 1, 1, false},
		{5, 10, 2, 0, true},
		{5, 10, 2, 0, true},

		{5, 5, 1, 1, false},
		{5, 5, 2, 0, true},
		{5, 5, 3, 0, true},

		{5, 3, 2, 2, false},
		{5, 3, 3, 0, true},

		{5, 2, 2, 2, false},
		{5, 2, 3, 3, false},
		{5, 2, 4, 0, true},
	}

	for _, c := range cases {
		p, err := validatePage(&c.page, c.perPage, c.totalCount)
		if c.expErr {
			assert.Error(t, err)
			continue
		}

		assert.Equal(t, c.newPage, p, fmt.Sprintf("%v", c))
	}

	// nil case
	p, err := validatePage(nil, 1, 1)
	if assert.NoError(t, err) {
		assert.Equal(t, 1, p)
	}
}

func TestPaginationPerPage(t *testing.T) {
	cases := []struct {
		totalCount int
		perPage    int
		newPerPage int
	}{
		{5, 0, defaultPerPage},
		{5, 1, 1},
		{5, 2, 2},
		{5, defaultPerPage, defaultPerPage},
		{5, maxPerPage - 1, maxPerPage - 1},
		{5, maxPerPage, maxPerPage},
		{5, maxPerPage + 1, maxPerPage},
	}

	for _, c := range cases {
		p := validatePerPage(&c.perPage)
		assert.Equal(t, c.newPerPage, p, fmt.Sprintf("%v", c))
	}

	// nil case
	p := validatePerPage(nil)
	assert.Equal(t, defaultPerPage, p)
}

func TestInitGenesisChunks(t *testing.T) {
	env = &Environment{}

	//
	// success
	//
	env.genChunks = nil
	env.GenDoc = &types.GenesisDoc{}
	err := InitGenesisChunks()
	require.NoError(t, err)

	env.genChunks = []string{}
	err = InitGenesisChunks()
	require.NoError(t, err)

	env.GenDoc = nil
	err = InitGenesisChunks()
	require.NoError(t, err)
}
