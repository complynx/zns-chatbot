package telegram

import "strconv"

// UserProfileURL ports client_user_link_url from zns-chatbot/telegram_links.py.
func UserProfileURL(userID int64, username string) string {
	if username != "" {
		return "https://t.me/" + username
	}
	return "tg://user?id=" + strconv.FormatInt(userID, 10)
}
