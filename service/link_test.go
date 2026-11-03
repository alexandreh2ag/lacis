package service

import (
	"encoding/json"
	"net/netip"
	"testing"

	"github.com/alexandreh2ag/lacis/cdn"
	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLinkService_InvalidCDNRange(t *testing.T) {
	appCtx := appContext.TestContext(nil)
	appCtx.Config.Linking.CDNRanges = []string{"cloudflare"}
	_, err := NewLinkService(appCtx, nil)
	assert.ErrorContains(t, err, "linking.cdn_ranges")
}

func TestNewLinkService_CDNs(t *testing.T) {
	appCtx := appContext.TestContext(nil)
	appCtx.Config.Linking.CDNs = []string{"cloudflare"}
	appCtx.Config.Linking.CDNRanges = []string{"198.51.100.7/24"}
	svc, err := NewLinkService(appCtx, nil)
	require.NoError(t, err)
	opts := svc.(*linkService).opts
	require.Len(t, opts.CDN, 2)
	assert.Equal(t, "cloudflare", opts.CDN[0].Name)
	assert.Equal(t, cdn.CDN{Name: cdn.Custom, Ranges: []netip.Prefix{netip.MustParsePrefix("198.51.100.0/24")}}, opts.CDN[1])

	appCtx.Config.Linking.CDNs = []string{"akamai"}
	_, err = NewLinkService(appCtx, nil)
	assert.ErrorContains(t, err, `linking.cdns: unknown CDN "akamai"`)
}

func TestLinkIDs(t *testing.T) {
	ns, app, pg := uuid.New(), uuid.New(), uuid.New()
	external := "api.partner.example"
	links := func() []model.Link {
		return []model.Link{
			{FromType: model.LinkNodeApp, FromID: app, ToType: model.LinkNodeService, ToID: &pg, Source: model.LinkSourceEnv,
				Detail: json.RawMessage(`{"key":"DATABASE_URL","rule":"private_ip"}`)},
			{FromType: model.LinkNodeApp, FromID: app, ToType: model.LinkNodeService, ToID: &pg, Source: model.LinkSourceEnv,
				Detail: json.RawMessage(`{"key":"DATABASE_READ_URL"}`)},
			{FromType: model.LinkNodeApp, FromID: app, ToType: model.LinkNodeService, ToID: &pg, Source: model.LinkSourceEnv,
				Detail: json.RawMessage(`{"key":"DATABASE_URL","rule":"vhost"}`)},
			{FromType: model.LinkNodeApp, FromID: app, ToType: model.LinkNodeExternal, ExternalTarget: &external, Source: model.LinkSourceEnv,
				Detail: json.RawMessage(`{"key":"PARTNER_URL"}`)},
		}
	}
	first, again, other := links(), links(), links()
	linkIDs(ns, first)
	linkIDs(ns, again)
	linkIDs(uuid.New(), other)
	seen := map[uuid.UUID]bool{}
	for i := range first {
		assert.NotEqual(t, uuid.Nil, first[i].ID)
		assert.Equal(t, first[i].ID, again[i].ID, "the same link keeps its id")
		assert.NotEqual(t, first[i].ID, other[i].ID, "another namespace, another id")
		assert.False(t, seen[first[i].ID], "a link of the same ends and key is told apart by its rank")
		seen[first[i].ID] = true
	}
}
