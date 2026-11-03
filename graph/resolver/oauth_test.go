package resolver_test

import (
	"errors"
	"testing"
	"time"

	mockService "github.com/alexandreh2ag/lacis/mocks/service"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestOAuthGrants(t *testing.T) {
	f := newFixture(t, reader())
	assert.JSONEq(t, `{"oauthGrants":[]}`, f.post(t, `{ oauthGrants { id } }`), "mcp off")
	assert.Equal(t, map[string]any{"code": "NOT_FOUND"}, extensions(t, f, `mutation { revokeOAuthGrant(id:"`+id(1).String()+`") }`))

	oauth := mockService.NewMockOAuthService(gomock.NewController(t))
	f.resolver.OAuthService = oauth
	grant := model.OAuthGrantView{ClientName: "Claude", OAuthGrant: model.OAuthGrant{Base: model.Base{ID: id(1)},
		CreatedAt: now, LastUsedAt: now.Add(time.Hour), ExpiresAt: now.Add(30 * 24 * time.Hour)}}
	oauth.EXPECT().Grants(gomock.Any(), id(90)).Return([]model.OAuthGrantView{grant}, nil)
	assert.JSONEq(t, `{"oauthGrants":[{"id":"`+id(1).String()+`","clientName":"Claude","createdAt":"2026-10-28T12:00:00Z",`+
		`"lastUsedAt":"2026-10-28T13:00:00Z","expiresAt":"2026-11-27T12:00:00Z"}]}`,
		f.post(t, `{ oauthGrants { id clientName createdAt lastUsedAt expiresAt } }`))
	oauth.EXPECT().Grants(gomock.Any(), id(90)).Return(nil, nil)
	assert.JSONEq(t, `{"oauthGrants":[]}`, f.post(t, `{ oauthGrants { id } }`))

	oauth.EXPECT().RevokeGrant(gomock.Any(), id(90), id(1)).Return(nil)
	assert.JSONEq(t, `{"revokeOAuthGrant":true}`, f.post(t, `mutation { revokeOAuthGrant(id:"`+id(1).String()+`") }`))
	oauth.EXPECT().RevokeGrant(gomock.Any(), id(90), id(2)).Return(service.ErrNotFound)
	assert.Equal(t, map[string]any{"code": "NOT_FOUND"}, extensions(t, f, `mutation { revokeOAuthGrant(id:"`+id(2).String()+`") }`))
}

func TestUserOAuthGrants(t *testing.T) {
	user := id(7)
	f := newFixture(t, reader())
	assert.Equal(t, map[string]any{"code": "FORBIDDEN"}, extensions(t, f, `{ userOAuthGrants(userId:"`+user.String()+`") { id } }`))
	assert.Equal(t, map[string]any{"code": "FORBIDDEN"}, extensions(t, f, `mutation { revokeUserOAuthGrant(userId:"`+user.String()+`", id:"`+id(1).String()+`") }`))

	// Reading the users is not writing them.
	usersReader := reader()
	usersReader.Global = []model.GlobalPermission{{Resource: model.ResourceUsers, Action: model.ActionRead}}
	f = newFixture(t, usersReader)
	oauth := mockService.NewMockOAuthService(gomock.NewController(t))
	f.resolver.OAuthService = oauth
	oauth.EXPECT().Grants(gomock.Any(), user).Return([]model.OAuthGrantView{{ClientName: "Claude", OAuthGrant: model.OAuthGrant{Base: model.Base{ID: id(1)}}}}, nil)
	assert.JSONEq(t, `{"userOAuthGrants":[{"id":"`+id(1).String()+`","clientName":"Claude"}]}`,
		f.post(t, `{ userOAuthGrants(userId:"`+user.String()+`") { id clientName } }`))
	assert.Equal(t, map[string]any{"code": "FORBIDDEN"}, extensions(t, f, `mutation { revokeUserOAuthGrant(userId:"`+user.String()+`", id:"`+id(1).String()+`") }`))

	f = newFixture(t, admin())
	assert.JSONEq(t, `{"userOAuthGrants":[]}`, f.post(t, `{ userOAuthGrants(userId:"`+user.String()+`") { id } }`), "mcp off")
	oauth = mockService.NewMockOAuthService(gomock.NewController(t))
	f.resolver.OAuthService = oauth
	oauth.EXPECT().RevokeGrant(gomock.Any(), user, id(1)).Return(nil)
	assert.JSONEq(t, `{"revokeUserOAuthGrant":true}`, f.post(t, `mutation { revokeUserOAuthGrant(userId:"`+user.String()+`", id:"`+id(1).String()+`") }`))
	oauth.EXPECT().RevokeGrant(gomock.Any(), user, id(2)).Return(service.ErrNotFound)
	assert.Equal(t, map[string]any{"code": "NOT_FOUND"}, extensions(t, f, `mutation { revokeUserOAuthGrant(userId:"`+user.String()+`", id:"`+id(2).String()+`") }`))
	oauth.EXPECT().Grants(gomock.Any(), user).Return(nil, errors.New("db down"))
	assert.Equal(t, map[string]any{"code": "INTERNAL_SERVER_ERROR"}, extensions(t, f, `{ userOAuthGrants(userId:"`+user.String()+`") { id } }`))
}
