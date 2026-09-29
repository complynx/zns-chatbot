package bot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/adminutilities"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

var adminFileID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,512}$`)
var adminFileExtension = regexp.MustCompile(`^\.[A-Za-z0-9]{1,12}$`)

func isAdminUtilityUpdate(in incoming, u telegram.Update) bool {
	if u.Message == nil {
		return false
	}
	parts := strings.Fields(in.text)
	if len(parts) == 0 {
		return false
	}
	switch parts[0] {
	case "/user_echo", "/get_file", "/refresh_events":
		return true
	}
	return false
}

func (b *Bot) handleAdminUtility(ctx context.Context, in incoming, u telegram.Update) error {
	prefs, err := b.API.Preferences(ctx, in.owner)
	if err != nil {
		return err
	}
	messages := &orderMessages{language: prefs.Language}
	var authorized struct {
		OK bool `json:"ok"`
	}
	err = b.API.call(ctx, in.owner, http.MethodGet, "/v1/admin-utilities/authorize", nil, &authorized)
	text := ""
	if err == nil {
		text, err = b.adminUtilityCommand(ctx, in, u, messages)
	}
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status < 500 {
		text = messages.text(i18n.AdminUtilityFailed, nil)
		err = nil
	}
	if err != nil {
		return err
	}
	if messages.err != nil {
		return messages.err
	}
	// Long diagnostic reports are split without truncating metadata or UTF-8.
	const chunkSize = 2000 // A rune can occupy two Telegram UTF-16 units.
	runes := []rune(text)
	for len(runes) > 0 {
		n := min(len(runes), chunkSize)
		if _, err = b.TG.Send(ctx, telegram.Send{ChatID: in.chat, Text: string(runes[:n])}); err != nil {
			return err
		}
		runes = runes[n:]
	}
	return nil
}

func (b *Bot) adminUtilityCommand(
	ctx context.Context,
	in incoming,
	u telegram.Update,
	m *orderMessages,
) (string, error) {
	parts := strings.Fields(u.Message.Text)
	if parts[0] == "/refresh_events" && len(parts) == 1 {
		var result adminutilities.Events
		err := b.API.call(ctx, in.owner, http.MethodPost, "/v1/admin-utilities/refresh", nil, &result)
		return m.text(
			i18n.AdminUtilityRefresh,
			map[string]string{"all": strings.Join(result.All, ", "), "active": strings.Join(result.Active, ", ")},
		), err
	}
	const commandWithArgument = 2
	if len(parts) != commandWithArgument {
		return m.text(i18n.AdminUtilityHelp, nil), nil
	}
	switch parts[0] {
	case "/user_echo":
		id, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return "", &core.ProblemError{Status: http.StatusBadRequest, Code: "invalid_user_id"}
		}
		var report adminutilities.Report
		err = b.API.call(
			ctx,
			in.owner,
			http.MethodGet,
			"/v1/admin-utilities/users/"+strconv.FormatInt(id, 10),
			nil,
			&report,
		)
		return adminUtilityReport(report, m), err
	case "/get_file":
		if !adminFileID.MatchString(parts[1]) {
			return m.text(i18n.AdminUtilityHelp, nil), nil
		}
		if err := b.adminUtilityFile(ctx, in.chat, parts[1]); err != nil {
			return "", &core.ProblemError{Status: http.StatusBadRequest, Code: "admin_file_unavailable"}
		}
		return "", nil
	default:
		return m.text(i18n.AdminUtilityHelp, nil), nil
	}
}

func (b *Bot) adminUtilityFile(ctx context.Context, chat int64, id string) error {
	var file telegram.File
	if err := b.TG.Call(ctx, "getFile", map[string]string{"file_id": id}, &file); err != nil {
		return err
	}
	filename := id
	if extension := path.Ext(file.Path); adminFileExtension.MatchString(extension) {
		filename += extension
	}
	body, err := b.TG.Download(ctx, telegram.Document{FileID: id, Size: file.Size})
	if err != nil {
		return err
	}
	_, err = b.TG.SendDocument(ctx, chat, filename, body)
	return err
}

func adminUtilityReport(report adminutilities.Report, m *orderMessages) string {
	lines := []string{m.text(i18n.AdminUtilityUser, map[string]string{"id": strconv.FormatInt(report.TelegramID, 10)})}
	if !report.Found {
		return strings.Join(append(lines, m.text(i18n.AdminUtilityMissing, nil)), "\n")
	}
	keys := make([]string, 0, len(report.Names))
	for key := range report.Names {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if report.Names[key] != "" {
			lines = append(lines, key+"="+report.Names[key])
		}
	}
	lines = append(
		lines,
		m.text(i18n.AdminUtilityRegistrationCount, map[string]string{"count": strconv.Itoa(len(report.Passes))}),
	)
	states := make([]string, 0, len(report.States))
	for state, count := range report.States {
		states = append(states, fmt.Sprintf("%s:%d", state, count))
	}
	sort.Strings(states)
	lines = append(lines, m.text(i18n.AdminUtilityRegistrationSummary, map[string]string{
		"states": strings.Join(
			states,
			", ",
		),
		"registered": strings.Join(report.Registered, ", "),
		"paid":       strings.Join(report.Paid, ", "),
	}))
	for _, pass := range report.Passes {
		lines = append(lines, string(pass))
	}
	latest := "—"
	if report.LatestRequest != nil {
		latest = report.LatestRequest.UTC().Format("2006-01-02 15:04:05 UTC")
	}
	lines = append(lines, m.text(i18n.AdminUtilityMessages, map[string]string{
		orderTotalKey: strconv.FormatInt(report.Requests, 10),
		"recent":      strconv.FormatInt(report.RecentRequests, 10),
		"replies":     strconv.FormatInt(report.Replies, 10),
		"latest":      latest,
	}), m.text(i18n.AdminUtilityLegacy, nil))
	return strings.Join(lines, "\n")
}
