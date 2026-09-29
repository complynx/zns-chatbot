package bot

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
)

func cacheMediaSelection(cached *cachedPlan, in incoming, input, requestInput agent.Input) {
	if input.MediaContext == nil {
		return
	}
	cached.MediaResolvedOrder, cached.MediaResolvedVersion = resolvedMediaOrder(in, cached.Plan, requestInput)
	cached.MediaResolvedFood = resolvedFoodReceipt(in, cached.Plan, requestInput)
	if event, version := resolvedRegistrationReceipt(in, cached.Plan, requestInput); event != "" {
		cached.MediaResolvedRegistration, cached.MediaResolvedVersion = event, version
	}
	cached.MediaCandidates = input.MediaContext.Candidates
	cached.MediaSelected = input.MediaContext.SelectedOrderID
	if cached.MediaID == "" && cached.Plan.View == agent.MediaView && len(input.MediaContext.Pending) == 1 {
		cached.MediaID = input.MediaContext.Pending[0].ID
	}
}

// A follow-up may select a displayed choice by meaning (for example "the first").
// Semantic references retain the displayed choice's version. An exact spoken ID
// may also select a current authorized candidate when it was not displayed.
func resolvedMediaOrder(in incoming, plan agent.Plan, input agent.Input) (string, int64) {
	proposal := plan.MediaAction
	if proposal == nil || input.MediaContext == nil ||
		proposal.Intent != mediaReceipt || proposal.OrderID == "" {
		return "", 0
	}
	evidence, voice := receiptFollowupEvidence(in, input, proposal.MediaID)
	if evidence == "" {
		return "", 0
	}
	for _, hint := range input.MediaContext.Pending {
		if hint.ID != proposal.MediaID {
			continue
		}
		for _, choice := range hint.Choices {
			if choice.Action == mediaOrderChoice && choice.OrderID == proposal.OrderID {
				return choice.OrderID, choice.Version
			}
		}
		// Exact spoken IDs have the same current-candidate path as typed IDs.
		// A displayed choice above always wins, retaining its original version.
		if voice && strings.Contains(evidence, proposal.OrderID) {
			if candidate := mediaCandidateByID(input.MediaContext.Candidates, proposal.OrderID); candidate != nil {
				return candidate.OrderID, candidate.Version
			}
		}
		return "", 0
	}
	return "", 0
}

// A current voice message can answer a previous attachment's question. The voice
// file itself must not become that receipt, and quoted audio/video is not a reply.
func receiptFollowupEvidence(in incoming, input agent.Input, target string) (string, bool) {
	if in.mediaID == "" {
		return in.text, false
	}
	if input.AV == nil || input.AV.ID != in.mediaID || target == in.mediaID || input.AV.Kind != "voice" ||
		input.AV.Transcript.Status != "ok" || strings.TrimSpace(input.AV.Transcript.Text) == "" {
		return "", false
	}
	return currentRequestEvidence(input), true
}

// Interpret replies against the last delivered card, never a freshly reordered list.
func (b *Bot) visibleMediaHint(ctx context.Context, owner, id, kind string) (agent.MediaHint, error) {
	var hint *agent.MediaHint
	err := b.DB.QueryRow(ctx, `SELECT rendered FROM bot.media_intake WHERE owner=$1 AND id=$2`, owner, id).Scan(&hint)
	if err != nil {
		return agent.MediaHint{}, err
	}
	if hint == nil {
		hint = &agent.MediaHint{ID: id, Kind: kind}
	}
	err = b.avHint(ctx, owner, hint)
	return *hint, err
}

func (b *Bot) mediaRecent(ctx context.Context, owner string) ([]agent.MediaEvent, error) {
	rows, err := b.DB.Query(ctx, `SELECT COALESCE(registration_command->>'event',''),id,status,last_action,last_origin,
COALESCE(command->>'order_id',''),COALESCE((command->>'version')::bigint,(registration_command->>'version')::bigint,0),notice
FROM bot.media_intake WHERE owner=$1 ORDER BY update_id DESC LIMIT 20`, owner)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[agent.MediaEvent])
}

// Choices share the same source with rendered buttons, without copying opaque
// callback tokens into model context.
func (b *Bot) mediaChoices(ctx context.Context, owner, language, status string) ([]agent.MediaChoice, error) {
	choices := []agent.MediaChoice{}
	if status == mediaDone {
		return choices, nil
	}
	if status == mediaChoose {
		candidates, err := b.mediaCandidates(ctx, owner)
		if err != nil {
			return nil, err
		}
		for _, candidate := range visibleReceiptCandidates(candidates) {
			if candidate.RegistrationEvent != "" {
				label, _ := i18n.Translate(language, i18n.RegistrationPayment, nil)
				title := candidate.RegistrationTitles[language]
				if title == "" {
					title = candidate.RegistrationEvent
				}
				choices = append(
					choices,
					agent.MediaChoice{
						Action:            registrationMediaChoice,
						RegistrationEvent: candidate.RegistrationEvent,
						Version:           candidate.Version,
						Label:             label + " · " + title + " · " + candidate.AmountRUB + " RUB",
					},
				)
				continue
			}
			choices = append(
				choices,
				agent.MediaChoice{Action: mediaOrderChoice, OrderID: candidate.OrderID, Version: candidate.Version,
					Label: candidate.OrderID + " · " + candidate.AmountBYN + " BYN / " + candidate.AmountRUB + " RUB"},
			)
		}
	}
	for _, entry := range []struct {
		action string
		label  i18n.ID
	}{
		{mediaReceipt, i18n.MediaReceipt}, {mediaAvatar, i18n.MediaAvatar}, {"other", i18n.MediaOther}, {mediaCancel, i18n.MediaCancel},
	} {
		if status == mediaChoose && entry.action == mediaReceipt {
			continue
		}
		label, err := i18n.Translate(language, entry.label, nil)
		if err != nil {
			return nil, err
		}
		choices = append(choices, agent.MediaChoice{Action: entry.action, Label: label})
	}
	return choices, nil
}
