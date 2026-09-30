package bot

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
)

func TestAdminMessageProgressEnglishAndRussian(t *testing.T) {
	t.Parallel()
	progress := adminmessage.JobProgress{
		Total:        55,
		Succeeded:    1,
		Queued:       2,
		Deferred:     3,
		Sending:      4,
		Rejected:     5,
		Cancelled:    6,
		Uncertain:    7,
		Parked:       8,
		Paused:       9,
		NotQueued:    10,
		SharedPaused: 2,
	}
	for _, test := range []struct{ language, prefix, uncertain, shared string }{
		{"en", "Whole broadcast:", "uncertain 7", "Shared service pause affects 2 queued/deferred recipients (included above)"},
		{"ru", "Вся рассылка:", "результат неизвестен 7", "Общая пауза доставки затрагивает 2 получателей в очереди или с отложенной попыткой (уже учтены выше)"},
	} {
		messages := orderMessages{language: test.language}
		text := adminMessageProgressText(progress, &messages)
		require.NoError(t, messages.err)
		require.True(t, strings.HasPrefix(text, test.prefix))
		require.Contains(t, text, test.uncertain)
		require.Contains(t, text, test.shared)
		require.NotContains(t, text, "{")
		for _, number := range []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"} {
			require.Contains(t, text, number)
		}
		recovered := progress
		recovered.SharedPaused = 0
		text = adminMessageProgressText(recovered, &messages)
		require.NoError(t, messages.err)
		require.Contains(t, text, strings.Replace(test.shared, "2", "0", 1))
		require.NotContains(t, text, "{")
	}
}
