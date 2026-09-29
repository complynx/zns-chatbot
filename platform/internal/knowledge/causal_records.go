package knowledge

import "context"

func factMemoryEntry(f Fact) MemoryEntry {
	return MemoryEntry{
		Namespace:  MemoryShared,
		Event:      f.Event,
		Topic:      f.Topic,
		Key:        f.Key,
		SourceKind: MemoryFactKind,
		Version:    f.Version,
		Text:       f.Text,
		Active:     f.Active,
	}
}
func memoMemoryEntry(m Memo) MemoryEntry {
	return MemoryEntry{
		Namespace:  MemoryPrivate,
		Topic:      memoryPrivateTopic(m.Key),
		Key:        m.Key,
		SourceKind: MemoryMemoKind,
		Version:    m.Version,
		Text:       m.Text,
		Active:     m.Active,
	}
}

func (s Service) authorizeFacts(ctx context.Context, actor string, facts []Fact) ([]Fact, error) {
	entries := make([]MemoryEntry, len(facts))
	for i, f := range facts {
		entries[i] = factMemoryEntry(f)
	}
	allowed, err := s.authorizeMemoryEntries(ctx, actor, entries)
	if err != nil {
		return nil, err
	}
	result := []Fact{}
	for _, f := range facts {
		for _, entry := range allowed {
			if compareMemoryEntries(factMemoryEntry(f), entry) == 0 {
				f.ReadAuthorities = entry.ReadAuthorities
				result = append(result, f)
				break
			}
		}
	}
	return result, nil
}

func (s Service) authorizeMemos(ctx context.Context, actor string, memos []Memo) ([]Memo, error) {
	entries := make([]MemoryEntry, len(memos))
	for i, m := range memos {
		entries[i] = memoMemoryEntry(m)
	}
	allowed, err := s.authorizeMemoryEntries(ctx, actor, entries)
	if err != nil {
		return nil, err
	}
	result := []Memo{}
	for _, m := range memos {
		for _, entry := range allowed {
			if compareMemoryEntries(memoMemoryEntry(m), entry) == 0 {
				m.ReadAuthorities = entry.ReadAuthorities
				result = append(result, m)
				break
			}
		}
	}
	return result, nil
}
