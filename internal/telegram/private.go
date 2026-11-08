package telegram

// PrivateUser returns only a valid private sender/chat pair, not an
// authorization decision. Unknown callback data still has a valid envelope.
func (u Update) PrivateUser() (User, bool) {
	var user User
	var chat Chat
	switch {
	case u.Message != nil && u.CallbackQuery == nil && u.Message.From != nil:
		user, chat = *u.Message.From, u.Message.Chat
	case u.CallbackQuery != nil && u.Message == nil && u.CallbackQuery.Message != nil:
		user, chat = u.CallbackQuery.From, u.CallbackQuery.Message.Chat
	default:
		return User{}, false
	}
	if u.ID < 0 || user.ID <= 0 || user.ID >= 1<<52 || user.IsBot || chat.Type != "private" || chat.ID != user.ID {
		return User{}, false
	}
	return user, true
}
