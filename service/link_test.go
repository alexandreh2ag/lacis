package service

import (
	"testing"

	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/stretchr/testify/assert"
)

func TestNewLinkService_InvalidCDNRange(t *testing.T) {
	appCtx := appContext.TestContext(nil)
	appCtx.Config.Linking.CDNRanges = []string{"cloudflare"}
	_, err := NewLinkService(appCtx, nil)
	assert.ErrorContains(t, err, "linking.cdn_ranges")
}
