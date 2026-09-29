package webappurl_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/webappurl"
)

func TestSubtreeRedirectDestinations(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, location, want string
		status               int
	}{
		{"slash", "/v1/?x=a%23b", "/a%2Fb/v1/?x=a%23b", http.StatusTemporaryRedirect},
		{"external", "https://login.example/v1/?x=a%23b", "https://login.example/v1/?x=a%23b", http.StatusTemporaryRedirect},
		{"authority", "//login.example/v1/?x=a%23b", "//login.example/v1/?x=a%23b", http.StatusTemporaryRedirect},
		{"mounted", "/a%2Fb/v1/?x=a%23b", "/a%2Fb/v1/?x=a%23b", http.StatusTemporaryRedirect},
		{"other-path", "/auth?next=1#part", "/auth?next=1#part", http.StatusTemporaryRedirect},
		{"other-status", "/v1/?x=a%23b", "/v1/?x=a%23b", http.StatusSeeOther},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := webappurl.SubtreeRedirects(
				"https://example.test/a%2Fb/miniapp/",
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Add("Set-Cookie", "session=value; Path=/a%2Fb/miniapp; HttpOnly")
					http.Redirect(w, r, test.location, test.status)
				}),
			)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1?x=a%23b", nil))
			require.Equal(t, test.status, recorder.Code)
			require.Equal(t, test.want, recorder.Header().Get("Location"))
			require.Equal(t, "session=value; Path=/a%2Fb/miniapp; HttpOnly", recorder.Header().Get("Set-Cookie"))
		})
	}
}
