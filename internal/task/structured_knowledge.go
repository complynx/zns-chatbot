package task

func (s *Service) structuredKnowledgeView(ru bool) string {
	if s.options.MiniAppURL == "" {
		return structuredText(ru,
			"The knowledge form is currently unavailable. Try /knowledge later.",
			"Форма базы знаний сейчас недоступна. Попробуй /knowledge позже.")
	}
	return structuredText(
		ru,
		"Open the knowledge base to read community and event articles, suggest a " +
"change or review a proposal. Opening the form does not publish or approv" +
"e anything.",
		"Открой базу знаний, чтобы читать статьи сообщества и эвентов, предложить" +
" изменение или проверить предложение. Открытие формы ничего не публикует" +
" и не подтверждает.",
	)
}
