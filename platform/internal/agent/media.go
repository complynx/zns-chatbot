package agent

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	mediaReceipt = "receipt"
	MediaView    = "media"
	mediaClarify = "clarify"
	mediaAvatar  = "avatar"
)

// MediaContext exposes only owner-authorized hints and current prices.
type MediaContext struct {
	LatestKnownReceipt *ReceiptSummary  `json:"latest_known_receipt"`
	Pending            []MediaHint      `json:"pending"`
	Candidates         []MediaCandidate `json:"candidates"`
	Recent             []MediaEvent     `json:"recent,omitempty"`
	SelectedOrderID    string           `json:"selected_order_id,omitempty"`
}

// ReceiptSummary links a committed proof action to its current order state.
// It describes the latest receipt in retained history, not the newest upload or order.
type ReceiptSummary struct {
	OrderID          string    `json:"order_id"`
	SubmittedAt      time.Time `json:"submitted_at"`
	SubmittedVersion int64     `json:"submitted_version"`
	Origin           string    `json:"origin"`
	CurrentState     string    `json:"current_state,omitempty"`
}

type MediaHint struct {
	Question   string        `json:"question,omitempty"`
	Choices    []MediaChoice `json:"choices,omitempty"`
	ID         string        `json:"id"`
	Kind       string        `json:"kind"`
	DurationMS int64         `json:"duration_ms"`
	CanInspect bool          `json:"can_inspect"`
}

// MediaChoice mirrors a currently visible host-generated clarification button.
type MediaChoice struct {
	FoodTarget        *FoodReceiptTarget `json:"food_target,omitempty"`
	RegistrationEvent string             `json:"registration_event,omitempty"`
	Action            string             `json:"action"`
	OrderID           string             `json:"order_id"`
	Version           int64              `json:"version"`
	Label             string             `json:"label"`
}

// FoodReceiptTarget binds a displayed choice to one independent payment generation.
type FoodReceiptTarget struct {
	EventID    string `json:"event_id"`
	OrderID    string `json:"order_id"`
	Kind       string `json:"kind"`
	Version    int64  `json:"version"`
	Generation int64  `json:"generation"`
}

// MediaEvent contains authoritative action metadata, never attachment contents.
type MediaEvent struct {
	RegistrationEvent string `json:"registration_event,omitempty"`
	ID                string `json:"id"`
	Status            string `json:"status"`
	Action            string `json:"action"`
	Origin            string `json:"origin"`
	OrderID           string `json:"order_id"`
	Version           int64  `json:"version"`
	Outcome           string `json:"outcome"`
}

type MediaCandidate struct {
	RegistrationTitles map[string]string `json:"registration_titles,omitempty"`
	RegistrationEvent  string            `json:"registration_event,omitempty"`
	OrderID            string            `json:"order_id"`
	Version            int64             `json:"version"`
	AmountBYN          string            `json:"amount_byn"`
	AmountRUB          string            `json:"amount_rub"`
}

// MediaProposal is interpretation evidence, never payment authorization.
// The host validates ownership and current state before using any proposal.
type MediaProposal struct {
	FoodKind          string `json:"food_kind,omitempty"`
	RegistrationEvent string `json:"registration_event"`
	MediaID           string `json:"media_id"`
	Intent            string `json:"intent"`
	Amount            string `json:"amount"`
	Currency          string `json:"currency"`
	OrderID           string `json:"order_id"`
	StartMS           int64  `json:"start_ms"`
	EndMS             int64  `json:"end_ms"`
	FrameCount        int    `json:"frame_count"`
}

var mediaAmount = regexp.MustCompile(`^(0|[1-9][0-9]{0,8})(\.[0-9]{1,2})?$`)

func validateMediaProposal(plan Plan) error {
	p := plan.MediaAction
	if p == nil {
		return nil
	}
	if plan.View != MediaView || plan.Action != nil || plan.OrderAction != nil || plan.ProfileAction != nil ||
		!validFoodMediaProposal(*p) {
		return errors.New("conflicting media proposal")
	}
	if !validMediaID(p.MediaID) || (p.OrderID != "" && !validMediaID(p.OrderID)) ||
		(p.RegistrationEvent != "" && (!validMediaID(p.RegistrationEvent) || p.OrderID != "")) {
		return errors.New("invalid media resource")
	}
	if p.Intent == "inspect_video" {
		return validateVideoInspection(*p)
	}
	if p.StartMS != 0 || p.EndMS != 0 || p.FrameCount != 0 {
		return errors.New("unexpected video inspection fields")
	}
	if p.Currency != "" && p.Currency != "BYN" && p.Currency != "RUB" {
		return errors.New("invalid media currency")
	}
	if p.Amount != "" && (!mediaAmount.MatchString(p.Amount) || strings.Trim(p.Amount, "0.") == "") {
		return errors.New("invalid media amount")
	}
	switch p.Intent {
	case mediaReceipt, mediaClarify:
		return nil
	case mediaAvatar, "other", "cancel":
		if p.Amount == "" && p.Currency == "" && p.OrderID == "" && p.RegistrationEvent == "" {
			return nil
		}
	}
	return errors.New("invalid media intent")
}

func validFoodMediaProposal(p MediaProposal) bool {
	if p.FoodKind == "" {
		return true
	}
	return (p.FoodKind == "meals" || p.FoodKind == "activities") &&
		p.Intent == mediaReceipt && p.OrderID != "" && p.RegistrationEvent == ""
}

func validateVideoInspection(p MediaProposal) error {
	if p.StartMS < 0 || p.StartMS >= p.EndMS || p.EndMS > 240000 || p.FrameCount < 1 || p.FrameCount > 8 ||
		p.Amount != "" || p.Currency != "" || p.OrderID != "" || p.RegistrationEvent != "" {
		return errors.New("invalid video inspection")
	}
	return nil
}

func validMediaID(id string) bool {
	return id != "" && len(id) <= 128 && utf8.ValidString(id) &&
		!strings.ContainsFunc(id, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) })
}

// The fixture reads explicit synthetic captions; it does not simulate vision.
func scriptedMediaPlan(in Input) (Plan, bool) {
	text := strings.TrimSpace(in.Text)
	id := ""
	if in.Attachment != nil {
		id = in.Attachment.ID
	} else if in.MediaContext != nil && (text == mediaAvatar || strings.HasPrefix(text, "receipt ")) &&
		len(in.MediaContext.Pending) == 1 {
		id = in.MediaContext.Pending[0].ID
	}
	if id == "" {
		return Plan{}, false
	}
	p := &MediaProposal{MediaID: id, Intent: mediaClarify}
	message := "Please clarify what you want to do with this image."
	if strings.HasPrefix(in.Language, "ru") {
		message = "Уточните, что вы хотите сделать с изображением."
	}
	if text == mediaAvatar {
		p.Intent = mediaAvatar
	} else {
		fields := strings.Fields(text)
		if len(fields) == 3 && fields[0] == mediaReceipt {
			p.Intent, p.Amount, p.Currency = mediaReceipt, fields[1], fields[2]
			if in.MediaContext != nil {
				p.OrderID = in.MediaContext.SelectedOrderID
			}
		}
	}
	plan := Plan{View: MediaView, Text: message, MediaAction: p}
	if validateMediaProposal(plan) != nil {
		plan.MediaAction = &MediaProposal{MediaID: id, Intent: mediaClarify}
	}
	return plan, true
}
