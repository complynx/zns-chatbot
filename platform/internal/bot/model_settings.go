package bot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const (
	modelUserCommand         = "/model_user"
	modelDefaultCommand      = "/model_default"
	modelAccessCommand       = "/model_access"
	modelSettingsCommandName = "/model"
	modelTargetArguments     = 2
	modelCallbackArguments   = 4
	modelValueArguments      = 2
)

func isModelSettingsUpdate(in incoming, _ telegram.Update) bool {
	p := strings.Fields(in.text)
	if len(p) == 0 {
		return false
	}
	switch p[0] {
	case modelSettingsCommandName, modelUserCommand, modelDefaultCommand, modelAccessCommand:
		return true
	}
	return strings.HasPrefix(in.text, "model:")
}
func (b *Bot) handleModelSettings(ctx context.Context, in incoming, u telegram.Update) error {
	prefs, err := b.API.Preferences(ctx, in.owner)
	if err != nil {
		return err
	}
	m := &orderMessages{language: prefs.Language}
	payload, err := b.modelSettingsCommand(ctx, in, u, m)
	if core.IsDatabaseFailure(err) {
		return core.ErrDatabase
	}
	if p, ok := errors.AsType[*core.ProblemError](err); ok && p.Status < 500 {
		id := i18n.ModelSettingsFailed
		if p.Status == http.StatusForbidden {
			id = i18n.ModelSettingsDenied
		}
		payload = telegram.Send{ChatID: in.chat, Text: m.text(id, nil)}
		err = nil
	}
	if err != nil {
		return err
	}
	if m.err != nil {
		return m.err
	}
	capability := modelsettings.Own
	parts := strings.Fields(in.text)
	if len(parts) > 0 {
		switch parts[0] {
		case modelUserCommand:
			capability = modelsettings.Others
		case modelDefaultCommand:
			capability = modelsettings.Global
		case modelAccessCommand:
			capability = modelsettings.GrantPermission
		}
	}
	ref := botdelivery.Reference{Family: botFamilyModelSettings, Object: capability}
	result := botdelivery.StoredResult{Payload: payload}
	if payload.Text == m.text(i18n.ModelSettingsDenied, nil) || payload.Text == m.text(i18n.ModelSettingsFailed, nil) {
		ref.Family = botFamilyStatic
		if payload.Text == m.text(i18n.ModelSettingsDenied, nil) {
			result.Notice = i18n.ModelSettingsDenied
		} else {
			result.Notice = i18n.ModelSettingsFailed
		}
	}
	if err = b.queueBotResult(ctx, in.owner, in.chat, u.ID, botFamilyModelSettings, ref, result, 0); err != nil {
		return err
	}
	if u.Callback != nil {
		if err = b.acknowledge(ctx, u.Callback.ID); err != nil {
			return err
		}
	}
	return b.record(ctx, in.owner, u.ID, botFamilyModelSettings, map[string]string{"text": payload.Text})
}

func (b *Bot) modelSettingsCommand(
	ctx context.Context,
	in incoming,
	u telegram.Update,
	m *orderMessages,
) (telegram.Send, error) {
	out := telegram.Send{ChatID: in.chat}
	var permissions map[string]bool
	if err := b.API.Call(
		ctx,
		in.owner,
		http.MethodGet,
		"/v1/model-settings/permissions",
		nil,
		&permissions,
	); err != nil {
		return out, err
	}
	p := strings.Fields(in.text)
	capability := modelsettings.Own
	endpoint := "/v1/model-settings"
	if p[0] == modelAccessCommand {
		return b.modelSettingsGrant(ctx, in, p, m, permissions)
	}
	if p[0] == modelDefaultCommand {
		capability = modelsettings.Global
		endpoint += "/default"
	}
	if p[0] == modelUserCommand {
		capability = modelsettings.Others
		if len(p) < modelTargetArguments {
			if !permissions[capability] {
				return out, modelSettingsForbidden()
			}
			out.Text = m.text(i18n.ModelSettingsOthersHelp, nil)
			return out, nil
		}
		endpoint += "/users/" + url.PathEscape(p[1])
		p = append(p[:1], p[2:]...)
	}
	if !permissions[capability] {
		if in.text == modelSettingsCommandName {
			return modelSettingsHelp(out, m, permissions)
		}
		return out, modelSettingsForbidden()
	}
	var state modelsettings.State
	if err := b.API.Call(ctx, in.owner, http.MethodGet, endpoint, nil, &state); err != nil {
		return out, err
	}
	if err := b.applyModelSettings(ctx, in, u, endpoint, p, &state); err != nil {
		return out, err
	}
	out.Text = m.text(
		i18n.ModelSettingsTitle,
		map[string]string{
			"model":           state.Effective.Model,
			"effort":          state.Effective.Effort,
			revisionParameter: strconv.FormatInt(state.Version, 10),
		},
	)
	if capability == modelsettings.Own {
		for _, option := range state.Catalog {
			for _, effort := range option.Efforts {
				out.Markup.Rows = append(
					out.Markup.Rows,
					[]telegram.Button{
						{
							Text: option.Model + " / " + effort,
							Data: fmt.Sprintf("model:%d:%s:%s", state.Version, option.Model, effort),
						},
					},
				)
			}
		}
		out.Markup.Rows = append(
			out.Markup.Rows,
			[]telegram.Button{
				{Text: m.text(i18n.ModelSettingsReset, nil), Data: fmt.Sprintf("model:%d:reset:", state.Version)},
			},
		)
	}
	help, _ := modelSettingsHelp(telegram.Send{}, m, permissions)
	out.Text += "\n" + help.Text
	return out, nil
}
func modelSettingsForbidden() error {
	return &core.ProblemError{Status: http.StatusForbidden, Code: mediaForbidden}
}
func modelSettingsInvalid() error {
	return &core.ProblemError{Status: http.StatusBadRequest, Code: "invalid_model_settings"}
}
func modelSettingsHelp(out telegram.Send, m *orderMessages, permissions map[string]bool) (telegram.Send, error) {
	for _, item := range []struct {
		permission string
		id         i18n.ID
	}{{modelsettings.Own, i18n.ModelSettingsOwnHelp}, {modelsettings.Others, i18n.ModelSettingsOthersHelp}, {modelsettings.Global, i18n.ModelSettingsGlobalHelp}, {modelsettings.GrantPermission, i18n.ModelSettingsGrantHelp}} {
		if permissions[item.permission] {
			out.Text += m.text(item.id, nil) + "\n"
		}
	}
	if out.Text == "" {
		return out, modelSettingsForbidden()
	}
	return out, nil
}

func (b *Bot) modelSettingsGrant(
	ctx context.Context,
	in incoming,
	p []string,
	m *orderMessages,
	permissions map[string]bool,
) (telegram.Send, error) {
	out := telegram.Send{ChatID: in.chat}
	if !permissions[modelsettings.GrantPermission] {
		return out, modelSettingsForbidden()
	}
	if len(p) != modelCallbackArguments || (p[3] != modelsettings.GrantPermission && p[3] != "revoke") {
		out.Text = m.text(i18n.ModelSettingsGrantHelp, nil)
		return out, nil
	}
	var result map[string]bool
	err := b.API.Call(
		ctx,
		in.owner,
		http.MethodPost,
		"/v1/model-settings/grants",
		modelsettings.Grant{Owner: p[1], Capability: p[2], Enabled: p[3] == modelsettings.GrantPermission},
		&result,
	)
	out.Text = m.text(i18n.ModelSettingsSaved, nil)
	return out, err
}

func (b *Bot) applyModelSettings(
	ctx context.Context,
	in incoming,
	u telegram.Update,
	endpoint string,
	parts []string,
	state *modelsettings.State,
) error {
	callback := u.Callback != nil && strings.HasPrefix(in.text, "model:")
	if callback {
		parts = strings.Split(in.text, ":")
	}
	if len(parts) <= 1 {
		return nil
	}
	change, err := parseModelSettingsChange(parts, callback, state.Version, u.ID)
	if err != nil {
		return err
	}
	return b.API.Call(ctx, in.owner, http.MethodPost, endpoint, change, state)
}
func parseModelSettingsChange(parts []string, callback bool, version, id int64) (modelsettings.Change, error) {
	change := modelsettings.Change{Version: version, OperationKey: "tg-model-" + strconv.FormatInt(id, 10)}
	values := parts[1:]
	if callback {
		if len(parts) != modelCallbackArguments {
			return change, modelSettingsInvalid()
		}
		value, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return change, modelSettingsInvalid()
		}
		change.Version = value
		values = parts[2:]
	}
	if len(values) > modelValueArguments {
		return change, modelSettingsInvalid()
	}
	if values[0] == "reset" {
		return change, nil
	}
	change.Model = values[0]
	if len(values) > 1 {
		change.Effort = values[1]
	}
	return change, nil
}

func (b *Bot) addModelSettingsMenu(ctx context.Context, owner, language string, payload *telegram.Send) error {
	var permissions map[string]bool
	if err := b.API.Call(ctx, owner, http.MethodGet, "/v1/model-settings/permissions", nil, &permissions); err != nil {
		if core.IsDatabaseFailure(err) {
			return core.ErrDatabase
		}
		// An optional menu must not prevent a safe reply during an ACL outage.
		// Show no settings controls unless the permission check succeeds.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return nil
	}
	if !permissions[modelsettings.Own] && !permissions[modelsettings.Others] && !permissions[modelsettings.Global] {
		return nil
	}
	text, err := i18n.Translate(language, i18n.ModelSettingsMenu, nil)
	if err != nil {
		return err
	}
	payload.Markup.Rows = append(payload.Markup.Rows, []telegram.Button{{Text: text, Data: modelSettingsCommandName}})
	return nil
}
