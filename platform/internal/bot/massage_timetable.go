package bot

import (
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
	"github.com/complynx/zns-chatbot/platform/internal/webappurl"
)

// Called only after the existing specialist or event administrator check.
func (r *massageRenderer) webTimetable() {
	if r.bot.WebAppURL == "" {
		return
	}
	address, err := webappurl.Route(r.bot.WebAppURL, "/miniapp/massage")
	if err != nil {
		return
	}

	query := url.Values{knowledgeEventQuery: {r.state.Event}, "lang": {r.language}}
	address.RawQuery = query.Encode()
	r.choices = append(r.choices, massageChoice{
		label: r.text(i18n.MassageWebTimetable), webApp: &telegram.WebApp{URL: address.String()},
	})
}
