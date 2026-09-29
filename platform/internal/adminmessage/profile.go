package adminmessage

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// broadcastProfile is private to authorized audience resolution and rendering.
// Source absence/null is retained; only explicit runtime overrides supersede it.
func (s Service) broadcastProfile(ctx context.Context, tx pgx.Tx, destination Destination) (map[string]any, error) {
	if destination.Thread != 0 {
		return nil, errors.New("template_topic_unsupported")
	}
	var raw []byte
	var id int64
	err := tx.QueryRow(ctx, `SELECT u.telegram_id,p.fields||p.overrides FROM core.users u JOIN core.admin_broadcast_profiles p ON p.owner=u.id
 WHERE u.telegram_id::text=$1 OR ($1 LIKE '@%' AND lower(u.username)=lower(substr($1,2)))`, destination.Chat).
		Scan(&id, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("template_profile_unavailable")
	}
	if err != nil {
		return nil, err
	}
	fields, err := decodeBroadcastProfile(raw)
	if err != nil {
		return nil, err
	}
	fields["user_id"] = id
	s.scopeProfile(fields)
	name := broadcastUserName(fields, id)
	fields["user_name"] = name
	username, _ := fields["username"].(string)
	fields["user_link_username"] = username
	link := "tg://user?id=" + strconv.FormatInt(id, 10)
	if username != "" {
		link = "https://t.me/" + username
	}
	fields[linkField] = `<a href="` + html.EscapeString(link) + `">` + html.EscapeString(name) + `</a>`
	if informal, present := fields["informal_name"]; present {
		fields["user_informal_name"] = informal
	}
	return fields, nil
}

// bot_id is deployment identity, never authority obtained from imported fields.
func (s Service) scopeProfile(fields map[string]any) {
	delete(fields, "bot_id")
	if s.BotID > 0 {
		fields["bot_id"] = s.BotID
	}
}

func decodeBroadcastProfile(raw []byte) (map[string]any, error) {
	var fields map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	err := decoder.Decode(&fields)
	if err == nil {
		templateNumericValues(fields)
	}
	return fields, err
}

// Preserve integral values as Go integers for comparisons; never silently round
// JSON numbers through float64. Non-integral values retain their exact spelling.
func templateNumericValues(value any) any {
	switch typed := value.(type) {
	case json.Number:
		if integer, err := typed.Int64(); err == nil {
			return integer
		}
	case map[string]any:
		for key, item := range typed {
			typed[key] = templateNumericValues(item)
		}
	case []any:
		for index, item := range typed {
			typed[index] = templateNumericValues(item)
		}
	}
	return value
}

func broadcastUserName(fields map[string]any, id int64) string {
	language, _ := fields["language_code"].(string)
	for _, key := range []string{"inner_name_" + language, "inner_name_en", "print_name"} {
		if value, ok := fields[key].(string); ok && value != "" {
			return value
		}
	}
	first, _ := fields["first_name"].(string)
	last, _ := fields["last_name"].(string)
	if name := strings.TrimSpace(first + " " + last); name != "" {
		return name
	}
	if username, ok := fields["username"].(string); ok && username != "" {
		return username
	}
	return strconv.FormatInt(id, 10)
}
