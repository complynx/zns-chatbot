package broadcasts_test

import (
	"testing"

	"github.com/complynx/zns-chatbot/internal/broadcasts"
)

func TestCategoryPolicy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		requested, assessed broadcasts.Category
		want                bool
	}{
		{broadcasts.Warning, broadcasts.Advertising, true},
		{broadcasts.Warning, broadcasts.Information, true},
		{broadcasts.Warning, broadcasts.Warning, false},
		{broadcasts.Information, broadcasts.Advertising, true},
		{broadcasts.Information, broadcasts.Information, false},
		{broadcasts.Advertising, broadcasts.Warning, false},
	}
	for _, c := range cases {
		if broadcasts.NeedsClarification(c.requested, c.assessed) != c.want {
			t.Fatalf("clarification for %s assessed as %s", c.requested, c.assessed)
		}
	}

	if broadcasts.Warning.Optional() || !broadcasts.Advertising.Optional() || !broadcasts.Information.Optional() ||
		broadcasts.Category("unknown").Valid() {
		t.Fatal("invalid subscription policy")
	}
}
