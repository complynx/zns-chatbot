package miniapp_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/miniapp"
)

func TestPublicMountAssets(t *testing.T) {
	t.Parallel()
	for _, mount := range []string{"", "/bot"} {
		t.Run(mount, func(t *testing.T) {
			t.Parallel()
			gateway := miniapp.Gateway{WebAppURL: "https://example.test" + mount + "/miniapp/?old=yes#ignored"}
			proxy := http.StripPrefix(mount, gateway.Handler())
			redirect := httptest.NewRecorder()
			proxy.ServeHTTP(redirect, httptest.NewRequest(http.MethodGet, mount+"/miniapp?order_id=a%2Fb", nil))
			require.Equal(t, http.StatusTemporaryRedirect, redirect.Code)
			require.Equal(t, mount+"/miniapp/?order_id=a%2Fb", redirect.Header().Get("Location"))
			for _, route := range []string{"/miniapp/", "/miniapp/massage", "/massage_timetable", "/menu"} {
				request := httptest.NewRequest(http.MethodGet, mount+route+"?lang=ru", nil)
				request.Header.Set("X-Forwarded-Prefix", "/evil")
				request.Header.Set("Forwarded", "host=evil.test;proto=http")
				response := httptest.NewRecorder()
				proxy.ServeHTTP(response, request)
				require.Equal(t, http.StatusOK, response.Code)
				links := regexp.MustCompile(`(?:src|href)="([^"]+)"`).FindAllStringSubmatch(response.Body.String(), -1)
				require.NotEmpty(t, links)
				for _, link := range links {
					require.Contains(t, link[1], mount+"/miniapp/")
					asset := httptest.NewRecorder()
					proxy.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, link[1], nil))
					require.Equal(t, http.StatusOK, asset.Code, link[1])
				}
			}
			photo := httptest.NewRecorder()
			proxy.ServeHTTP(photo, httptest.NewRequest(http.MethodGet, mount+"/miniapp/foodphotos/fish.jpg", nil))
			require.Equal(t, http.StatusOK, photo.Code)
			require.Equal(t, "image/jpeg", photo.Header().Get("Content-Type"))
		})
	}
}
