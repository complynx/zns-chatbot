package knowledge

// projectReplayEvidence removes legacy expanded evidence after authorization.
// Opaque output leaves resolve the retained closure inside the source service.
func projectReplayEvidence(actor string, result *Result) error {
	if result.Proposal != nil && len(result.Proposal.ReadAuthorities) > 0 {
		refs, err := proposalReadAuthorities(*result.Proposal)
		if err != nil {
			return err
		}
		result.Proposal.ReadAuthorities = refs
	}
	ref, _ := resultMemoryReference(*result)
	if ref.Namespace != "" {
		entry, err := memoryEntryEvidence(
			actor,
			MemoryEntry{
				Namespace:  ref.Namespace,
				Event:      ref.Event,
				Topic:      ref.Topic,
				Key:        ref.Key,
				SourceKind: ref.SourceKind,
				Version:    ref.Version,
			},
			memoryCausalRecord{found: true},
		)
		if err != nil {
			return err
		}
		if result.Fact != nil && len(result.Fact.ReadAuthorities) > 0 {
			result.Fact.ReadAuthorities = entry.ReadAuthorities
		}
		if result.Memo != nil && len(result.Memo.ReadAuthorities) > 0 {
			result.Memo.ReadAuthorities = entry.ReadAuthorities
		}
		if result.Document != nil && len(result.Document.ReadAuthorities) > 0 {
			result.Document.ReadAuthorities = entry.ReadAuthorities
		}
	}
	if len(result.ReadAuthorities) > 0 {
		result.ReadAuthorities = nil
		refs, err := memoryResultAuthorities(*result)
		if err != nil {
			return err
		}
		result.ReadAuthorities = refs
	}
	return nil
}
