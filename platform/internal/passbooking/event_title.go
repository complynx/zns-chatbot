package passbooking

import (
	"slices"
	"strings"
)

func (e Event) Title(locale string, short bool) string {
	titles := e.Titles
	if short && len(e.ShortTitles) > 0 {
		titles = e.ShortTitles
	}
	locale = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(locale)), "_", "-")
	base, _, _ := strings.Cut(locale, "-")
	keys := []string{locale, base}
	regional := []string{}
	for key := range titles {
		if strings.HasPrefix(key, base+"-") {
			regional = append(regional, key)
		}
	}
	slices.Sort(regional)
	keys = append(keys, regional...)
	keys = append(keys, "en", "ru", "default")
	for _, key := range keys {
		if titles[key] != "" {
			return titles[key]
		}
	}
	return e.ID
}
