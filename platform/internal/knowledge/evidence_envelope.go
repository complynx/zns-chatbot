package knowledge

import (
	"slices"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// EvidenceEnvelope gives wire collections one bounded evidence union instead of repeating it
// beside each body. Domain snapshots retain their exact per-item evidence.
func EvidenceEnvelope(value any) (any, error) {
	switch value := value.(type) {
	case []Memo:
		page := MemoPage{Items: slices.Clone(value)}
		groups := [][]readsource.Authority{}
		for i := range page.Items {
			groups = append(groups, page.Items[i].ReadAuthorities)
			page.Items[i].ReadAuthorities = nil
		}
		var err error
		page.ReadAuthorities, err = readsource.Merge(groups...)
		return page, err
	case []Proposal:
		page := ProposalPage{Items: slices.Clone(value)}
		groups := [][]readsource.Authority{}
		for i := range page.Items {
			groups = append(groups, page.Items[i].ReadAuthorities)
			page.Items[i].ReadAuthorities = nil
		}
		var err error
		page.ReadAuthorities, err = readsource.Merge(groups...)
		return page, err
	case []Fact:
		return knowledgeFactEnvelope(FactPage{Facts: value})
	case FactPage:
		return knowledgeFactEnvelope(value)
	case MemoryPage:
		var err error
		value.Entries, value.ReadAuthorities, err = memoryEvidenceEnvelope(value.Entries, value.ReadAuthorities)
		return value, err
	case MemoryOverview:
		var err error
		value.Summaries, value.ReadAuthorities, err = memoryEvidenceEnvelope(value.Summaries, value.ReadAuthorities)
		return value, err
	case conversation.Page:
		value.Events = slices.Clone(value.Events)
		groups := [][]readsource.Authority{value.ReadAuthorities}
		for i := range value.Events {
			groups = append(groups, value.Events[i].ReadAuthorities)
			value.Events[i].ReadAuthorities = nil
		}
		var err error
		value.ReadAuthorities, err = readsource.Merge(groups...)
		return value, err
	case Result:
		return knowledgeResultEnvelope(value)
	default:
		return value, nil
	}
}

func knowledgeFactEnvelope(page FactPage) (FactPage, error) {
	page.Facts = slices.Clone(page.Facts)
	groups := [][]readsource.Authority{page.ReadAuthorities}
	for i := range page.Facts {
		groups = append(groups, page.Facts[i].ReadAuthorities)
		page.Facts[i].ReadAuthorities = nil
	}
	var err error
	page.ReadAuthorities, err = readsource.Merge(groups...)
	return page, err
}

func memoryEvidenceEnvelope(
	entries []MemoryEntry,
	refs []readsource.Authority,
) ([]MemoryEntry, []readsource.Authority, error) {
	entries = slices.Clone(entries)
	groups := [][]readsource.Authority{refs}
	for i := range entries {
		groups = append(groups, entries[i].ReadAuthorities)
		entries[i].ReadAuthorities = nil
	}
	union, err := readsource.Merge(groups...)
	return entries, union, err
}

func knowledgeResultEnvelope(result Result) (Result, error) {
	groups := [][]readsource.Authority{result.ReadAuthorities}
	if result.Fact != nil {
		value := *result.Fact
		groups = append(groups, value.ReadAuthorities)
		value.ReadAuthorities = nil
		result.Fact = &value
	}
	if result.Memo != nil {
		value := *result.Memo
		groups = append(groups, value.ReadAuthorities)
		value.ReadAuthorities = nil
		result.Memo = &value
	}
	if result.Document != nil {
		value := *result.Document
		groups = append(groups, value.ReadAuthorities)
		value.ReadAuthorities = nil
		result.Document = &value
	}
	if result.Proposal != nil {
		value := *result.Proposal
		groups = append(groups, value.ReadAuthorities)
		value.ReadAuthorities = nil
		result.Proposal = &value
	}
	var err error
	result.ReadAuthorities, err = readsource.Merge(groups...)
	return result, err
}
