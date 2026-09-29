package bot

import (
	"fmt"
	"strconv"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

const tierPercent = 100

const tierMoscowOffset = 3 * time.Hour

type tierText struct {
	language string
	messages []string
	err      error
}

func (t *tierText) label(id i18n.ID) string {
	if t.err != nil {
		return ""
	}
	var text string
	text, t.err = i18n.Translate(t.language, id, nil)
	return text
}
func (t *tierText) add(id i18n.ID, values map[string]string) {
	if t.err != nil {
		return
	}
	var text string
	text, t.err = i18n.Translate(t.language, id, values)
	if t.err == nil {
		t.messages = append(t.messages, text)
	}
}
func tierCounts(counts passallocation.Counts) string {
	return fmt.Sprintf("%d | %d", counts.Leader, counts.Follower)
}
func tierBalance(counts passallocation.Counts) string {
	leader, follower := 0.0, 0.0
	if total := counts.Leader + counts.Follower; total > 0 {
		leader = float64(counts.Leader) * tierPercent / float64(total)
		follower = float64(counts.Follower) * tierPercent / float64(total)
	}
	return fmt.Sprintf("%s (%.1f%% | %.1f%%)", tierCounts(counts), leader, follower)
}
func passTierMessages(language string, result passbooking.TierStatus) ([]string, error) {
	text := tierText{language: language}
	if len(result.Tiers) == 0 {
		text.add(i18n.RegistrationTierNoTiers, map[string]string{knowledgeEventQuery: result.Event})
		return text.messages, text.err
	}
	target := i18n.RegistrationTierTargetNone
	if result.Target == passallocation.Leader {
		target = i18n.RegistrationTierScopeLeader
	}
	if result.Target == passallocation.Follower {
		target = i18n.RegistrationTierScopeFollower
	}
	text.add(i18n.RegistrationTierSummary, map[string]string{
		knowledgeEventQuery: result.Event,
		"rule":              string(result.Rule),
		"balance":           tierBalance(result.Balance),
		"fullbalance":       tierBalance(result.FullBalance),
		"assigned":          tierCounts(result.Assigned),
		"waiting": tierCounts(
			result.Waiting,
		),
		"waitingtotal":      strconv.Itoa(result.Waiting.Leader + result.Waiting.Follower),
		"target":            text.label(target),
		"participants":      tierCounts(result.Participants),
		"participantstotal": strconv.Itoa(result.Participants.Leader + result.Participants.Follower),
	})
	if result.Rule == passallocation.Distributed {
		text.report(i18n.RegistrationTierScopeCurrent, result.Distributed)
		text.report(i18n.RegistrationTierScopeCouple, passallocation.TierReport{Current: result.Couple})
	} else {
		text.report(i18n.RegistrationTierScopeLeader, result.Leader)
		text.report(i18n.RegistrationTierScopeFollower, result.Follower)
	}
	return text.messages, text.err
}
func (t *tierText) report(scopeID i18n.ID, report passallocation.TierReport) {
	scope := t.label(scopeID)
	detail := report.Current
	if detail == nil {
		t.add(i18n.RegistrationTierNone, map[string]string{"scope": scope})
		detail = report.Next
	}
	if detail == nil {
		return
	}
	values := map[string]string{
		"scope":            scope,
		passBatchTier:      strconv.Itoa(detail.Number),
		passPriceParameter: strconv.Itoa(detail.Tier.Price),
		"left":             strconv.Itoa(detail.Left),
		"amount":           strconv.Itoa(detail.Capacity),
		"used":             strconv.Itoa(detail.Used),
		"explicit":         strconv.Itoa(detail.Explicit),
		"promo":            strconv.FormatBool(detail.Tier.Promo),
		"blocked":          strconv.FormatBool(detail.Tier.BlockedByDate),
		"starts": detail.Tier.Start.In(time.FixedZone("MSK", int(tierMoscowOffset.Seconds()))).
			Format("2006-01-02 15:04 MST"),
		"overflow": strconv.FormatBool(detail.Overflow),
	}
	if report.Current != nil {
		t.add(i18n.RegistrationTierDetail, values)
		return
	}
	t.add(i18n.RegistrationTierNext, values)
	if !detail.Future {
		return
	}
	if detail.Tier.BlockedByDate {
		t.add(i18n.RegistrationTierGateDate, values)
		return
	}
	values["threshold"] = strconv.Itoa(detail.PriorCapacity)
	values["participants"] = strconv.Itoa(detail.Participants)
	t.add(i18n.RegistrationTierGateCapacity, values)
}
