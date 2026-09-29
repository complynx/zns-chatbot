package knowledge

import (
	"context"
	"encoding/base64"
	"encoding/json"
)

func (s Service) Retrieve(ctx context.Context, actor string, q Query) ([]Fact, error) {
	page, err := s.RetrievePage(ctx, actor, q)
	return page.Facts, err
}

func (s Service) RetrievePage(ctx context.Context, actor string, q Query) (FactPage, error) {
	facts, err := s.retrieve(ctx, actor, q)
	if err != nil {
		return FactPage{}, err
	}
	page := FactPage{Facts: facts, More: len(facts) > MaxResults}
	if page.More {
		page.Facts = page.Facts[:MaxResults]
	}
	if len(page.Facts) > 0 {
		page.NextCursor = CursorAfter(page.Facts[len(page.Facts)-1])
	}
	return page, nil
}

func CursorAfter(fact Fact) string {
	data, _ := json.Marshal([2]string{fact.Topic, fact.Key})
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeFactCursor(value string) (string, string, error) {
	if value == "" {
		return "", "", nil
	}
	const maxCursorBytes = 300
	if len(value) > maxCursorBytes {
		return "", "", invalid()
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", "", invalid()
	}
	var keys []string
	if json.Unmarshal(data, &keys) != nil || len(keys) != 2 || !validID(keys[0], false) || !validID(keys[1], false) {
		return "", "", invalid()
	}
	return keys[0], keys[1], nil
}
