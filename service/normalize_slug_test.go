package service

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/alexandreh2ag/lacis/common/labels"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestAppSlug(t *testing.T) {
	var logs bytes.Buffer
	n := &normalizer{
		log:  slog.New(slog.NewTextHandler(&logs, nil)),
		host: &model.Host{Base: model.Base{ID: uuid.New()}},
		snap: &snapshot.Snapshot{Host: snapshot.Host{Labels: labels.Labels{Namespace: "acme", Project: "shop", Env: "prod"}}},
	}
	app := snapshot.App{Path: "/var/www/x"}
	assert.Equal(t, "site-v2", n.appSlug(app, labels.KeyProject, "site-v2"))
	assert.Equal(t, "", n.appSlug(app, labels.KeyEnv, ""), "empty: no env")
	assert.Empty(t, logs.String())
	assert.Equal(t, "shop", n.appSlug(app, labels.KeyProject, "Shop Admin"))
	assert.Equal(t, "prod", n.appSlug(app, labels.KeyEnv, "../prod"))
	assert.Contains(t, logs.String(), "app label is not a slug")
	n.log = nil
	assert.Equal(t, "prod", n.appSlug(app, labels.KeyEnv, "PROD"))
}
