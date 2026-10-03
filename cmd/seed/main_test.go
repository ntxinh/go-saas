package main

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFlags(t *testing.T) {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	o, err := parseFlags(fs, []string{"-orgs", "3", "-users", "7", "-local"})
	require.NoError(t, err)
	assert.Equal(t, 3, o.orgs)
	assert.Equal(t, 7, o.users)
	assert.True(t, o.local)

	fs = flag.NewFlagSet("seed", flag.ContinueOnError)
	o, err = parseFlags(fs, nil)
	require.NoError(t, err)
	assert.Equal(t, 5, o.orgs)
	assert.Equal(t, 10, o.users)
	assert.False(t, o.local)
}
