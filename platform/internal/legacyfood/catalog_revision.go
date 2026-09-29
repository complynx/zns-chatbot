package legacyfood

import "encoding/json"

// CatalogRevision binds actual menu contents and prices, not the import-time
// menu checksum, which need not change when an administrator edits the catalog.
func CatalogRevision(event Event) (string, error) {
	data, err := json.Marshal(struct {
		Menu       json.RawMessage `json:"menu"`
		Meals      MealPrices      `json:"meals"`
		Activities ActivityPrices  `json:"activities"`
	}{event.Menu, event.MealPrices, event.ActivityPrices})
	if err != nil {
		return "", err
	}
	return digest(data), nil
}

func checkCatalogRevision(event Event, expected string) error {
	// Older manual/browser commands never promised a catalog revision.
	if expected == "" {
		return nil
	}
	actual, err := CatalogRevision(event)
	if err != nil {
		return err
	}
	if actual != expected {
		return problem("food_catalog_stale")
	}
	return nil
}
