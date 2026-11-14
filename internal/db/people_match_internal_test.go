package db

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPeopleNameMatching(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		query, name string
		match       bool
	}{
		{"ivanov ivan", "ivan petrovich ivanov", true},
		{"anan ivanova", "anna ivanova", true},
		{"ivnova", "ivanova", true},
		{"ivan", "ivanov", true},
		{"anna anna", "anna ivanova", false},
		{"petrov petr", "ivan ivanov", false},
		{"al", "bo", false},
		{"acbdfeg", "abcdefg", true},
		{"ivanov i i", "ivan ivanovich ivanov", true},
		{"anna i", "anna ivanova", true},
		{"anna p", "anna ivanova", false},
		{"i i", "ivan petr", false},
	} {
		t.Run(tt.query, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.match, nameMatch(strings.Fields(tt.query), strings.Fields(tt.name)) > 0)
		})
	}
	require.Greater(
		t,
		nameMatch(strings.Fields("Anna"), strings.Fields("Anna")),
		nameMatch(strings.Fields("Anan"), strings.Fields("Anna")),
	)
}
