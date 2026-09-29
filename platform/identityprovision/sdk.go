package identityprovision

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"net"
	"net/url"
	"time"

	filter "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/filter/v2"
	metadata "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/metadata/v2"
	user "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/user/v2"
	"golang.org/x/oauth2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	grpcoauth "google.golang.org/grpc/credentials/oauth"
	"google.golang.org/grpc/status"
)

const (
	operationKey         = "zns.provisioning.operation"
	providerTimeout      = 15 * time.Second
	initialPasswordBytes = 32
)

// SDK uses official generated v4.16-compatible APIs. Tokens must belong to a
// separate organization-scoped provisioner, never the runtime impersonator.
type SDK struct {
	Client user.UserServiceClient
	conn   *grpc.ClientConn
}

type SDKConfig struct {
	Issuer         string
	TokenSource    oauth2.TokenSource
	AllowLocalHTTP bool
}

func NewSDK(config SDKConfig) (*SDK, error) {
	u, err := url.Parse(config.Issuer)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") || config.TokenSource == nil {
		return nil, ErrInvalid
	}
	var transport credentials.TransportCredentials
	secure := true
	switch {
	case u.Scheme == "https":
		transport = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	case config.AllowLocalHTTP && u.Scheme == "http" && (u.Hostname() == "localhost" || net.ParseIP(u.Hostname()).IsLoopback()):
		transport = insecure.NewCredentials()
		secure = false
	default:
		return nil, ErrInvalid
	}
	target := u.Host
	if u.Port() == "" {
		port := "443"
		if !secure {
			port = "80"
		}
		target = net.JoinHostPort(u.Hostname(), port)
	}
	conn, err := grpc.NewClient(
		target,
		grpc.WithTransportCredentials(transport),
		grpc.WithPerRPCCredentials(
			sdkToken{
				TokenSource: grpcoauth.TokenSource{TokenSource: oauth2.ReuseTokenSource(nil, config.TokenSource)},
				secure:      secure,
			},
		),
	)
	if err != nil {
		return nil, ErrUnavailable
	}
	return &SDK{Client: user.NewUserServiceClient(conn), conn: conn}, nil
}

type sdkToken struct {
	grpcoauth.TokenSource

	secure bool
}

func (t sdkToken) RequireTransportSecurity() bool { return t.secure }

func (t sdkToken) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	if t.secure {
		return t.TokenSource.GetRequestMetadata(ctx, uri...)
	}
	// The constructor permits this path only for explicit loopback stand access.
	token, err := t.TokenSource.Token()
	if err != nil {
		return nil, ErrUnavailable
	}
	return map[string]string{"authorization": token.Type() + " " + token.AccessToken}, nil
}

func (s *SDK) Close() error {
	if s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

func (s *SDK) Get(ctx context.Context, subject string) (Account, error) {
	ctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	response, err := s.Client.GetUserByID(ctx, &user.GetUserByIDRequest{UserId: subject})
	if err != nil {
		return Account{}, providerError(err)
	}
	u := response.GetUser()
	result := Account{Subject: u.GetUserId(), Organization: u.GetDetails().GetResourceOwner(),
		Human: u.GetHuman() != nil, Active: u.GetState() == user.UserState_USER_STATE_ACTIVE}
	meta, err := s.Client.ListUserMetadata(ctx, &user.ListUserMetadataRequest{UserId: subject,
		Filters: []*metadata.MetadataSearchFilter{{Filter: &metadata.MetadataSearchFilter_KeyFilter{
			KeyFilter: &metadata.MetadataKeyFilter{
				Key:    operationKey,
				Method: filter.TextFilterMethod_TEXT_FILTER_METHOD_EQUALS,
			},
		}}}})
	if err != nil {
		return Account{}, providerError(err)
	}
	for _, entry := range meta.GetMetadata() {
		if entry.GetKey() == operationKey {
			if result.Operation != "" {
				return Account{}, ErrConflict
			}
			result.Operation = string(entry.GetValue())
		}
	}
	return result, nil
}

func (s *SDK) Create(ctx context.Context, c Creation) error {
	ctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	secret := make([]byte, initialPasswordBytes)
	if _, err := rand.Read(secret); err != nil {
		return ErrUnavailable
	}
	password := base64.RawURLEncoding.EncodeToString(secret) + "aA1!"
	lastName := c.LastName
	if lastName == "" {
		lastName = c.FirstName
	}
	response, err := s.Client.CreateUser(ctx, &user.CreateUserRequest{
		OrganizationId: c.Organization, UserId: &c.Subject, Username: &c.Subject,
		UserType: &user.CreateUserRequest_Human_{Human: &user.CreateUserRequest_Human{
			Profile: &user.SetHumanProfile{
				GivenName:         c.FirstName,
				FamilyName:        lastName,
				PreferredLanguage: &c.Language,
			},
			Email: &user.SetHumanEmail{
				Email:        c.Email,
				Verification: &user.SetHumanEmail_ReturnCode{ReturnCode: &user.ReturnEmailVerificationCode{}},
			},
			PasswordType: &user.CreateUserRequest_Human_Password{Password: &user.Password{Password: password}},
		}}, Metadata: []*user.Metadata{{Key: operationKey, Value: []byte(c.Operation)}},
	})
	if err != nil {
		return providerError(err)
	}
	if response.GetId() != c.Subject {
		return ErrConflict
	}
	return nil
}

func providerError(err error) error {
	code := status.Code(err)
	if code == codes.NotFound {
		return ErrNotFound
	}
	if code == codes.AlreadyExists || code == codes.FailedPrecondition {
		return ErrConflict
	}
	return ErrUnavailable
}
