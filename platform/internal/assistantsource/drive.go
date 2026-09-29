package assistantsource

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2/jwt"

	"github.com/complynx/zns-chatbot/platform/internal/config"
)

const FetchTimeout = 30 * time.Second
const driveScope = "https://www.googleapis.com/auth/drive.readonly"

type fetchTransport func(*http.Request) (*http.Response, error)

func (f fetchTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// Drive keeps authentication inside the Core boundary. Transport injection is
// for synthetic tests; production URLs and the OAuth token endpoint are fixed.
type Drive struct {
	auth      *jwt.Config
	document  string
	transport http.RoundTripper
	identity  string
}

func NewDrive(settings config.AssistantSources, transport http.RoundTripper) (*Drive, error) {
	raw, err := base64.StdEncoding.DecodeString(settings.Credentials.Value())
	if err != nil {
		return nil, ErrInvalid
	}
	auth, err := google.JWTConfigFromJSON(raw, driveScope)
	if err != nil || auth.Email == "" || len(auth.PrivateKey) == 0 ||
		(auth.TokenURL != "https://oauth2.googleapis.com/token" && auth.TokenURL != google.JWTTokenURL) ||
		settings.AboutDocument == "" || len(settings.AboutDocument) > 200 || strings.ContainsAny(settings.AboutDocument, "/\\?# \t\r\n") {
		return nil, ErrInvalid
	}
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &Drive{auth: auth, document: settings.AboutDocument, transport: transport,
		identity: Digest([]byte("drive\x00" + settings.AboutDocument + "\x00" + Digest(raw)))}, nil
}

func (d *Drive) Identity() string { return d.identity }

// Fetch creates a library token source bound to this fetch's deadline, including
// token acquisition. No upstream errors or response bodies escape this boundary.
func (d *Drive) Fetch(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()
	// jwt's token POST does not attach its token-source context to the request.
	// Bind every library request to this fetch before it reaches the transport.
	transport := fetchTransport(func(request *http.Request) (*http.Response, error) {
		return d.transport.RoundTrip(request.WithContext(ctx))
	})
	base := &http.Client{Transport: transport, Timeout: FetchTimeout, CheckRedirect: noRedirect}
	authContext := context.WithValue(ctx, oauth2.HTTPClient, base)
	client := d.auth.Client(authContext)
	client.Timeout = FetchTimeout
	client.CheckRedirect = noRedirect
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://www.googleapis.com/drive/v3/files/"+url.PathEscape(d.document)+"/export?mimeType=text%2Fmarkdown",
		nil,
	)
	if err != nil {
		return nil, ErrInvalid
	}
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrFetch
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, ErrFetch
	}
	data, err := readBounded(response.Body)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return data, err
}

func noRedirect(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
