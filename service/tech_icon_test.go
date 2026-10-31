package service

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/auth"
	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/icon"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gorm.io/gorm"
)

func pngOf(t *testing.T, w, h int) []byte {
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))))
	return b.Bytes()
}

func TestTech(t *testing.T) {
	for raw, want := range map[string]string{" Traefik ": "traefik", "php-fpm": "php-fpm", "next.js": "next.js", "c++": "c++"} {
		got, err := Tech(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got)
	}
	for _, raw := range []string{"", " ", "-redis", "a/b", "../x", "red is"} {
		_, err := Tech(raw)
		assert.ErrorIs(t, err, ErrInvalid, raw)
	}
}

func TestTechIconService_Set(t *testing.T) {
	m := newMocks(t)
	actor := &auth.Principal{User: model.User{Base: model.Base{ID: uuid.New()}, Username: "admin"}}
	data := pngOf(t, 64, 64)
	m.techIcon.EXPECT().Save(ctx, gomock.Any()).DoAndReturn(func(_ any, i *model.TechIcon) error {
		assert.Equal(t, "traefik", i.Tech)
		assert.Equal(t, "image/png", i.ContentType)
		assert.Equal(t, data, i.Content)
		assert.Equal(t, actor.User.ID, *i.UpdatedBy)
		assert.Equal(t, now, i.UpdatedAt)
		return nil
	})
	got, err := NewTechIconService(testContext(t), m.tx).Set(ctx, actor, " Traefik ", "traefik.png", data)
	require.NoError(t, err)
	assert.Equal(t, "admin", *got.UpdatedByName)

	_, err = NewTechIconService(testContext(t), m.tx).Set(ctx, actor, "traefik", "traefik.png", pngOf(t, 600, 64))
	assert.ErrorIs(t, err, ErrInvalid)
	assert.ErrorIs(t, err, icon.ErrDimension)
	_, err = NewTechIconService(testContext(t), m.tx).Set(ctx, actor, "a/b", "x.png", data)
	assert.ErrorIs(t, err, ErrInvalid)

	errDB := errors.New("db down")
	m.techIcon.EXPECT().Save(ctx, gomock.Any()).Return(errDB)
	_, err = NewTechIconService(testContext(t), m.tx).Set(ctx, nil, "redis", "redis.svg", []byte(`<svg/>`))
	assert.ErrorIs(t, err, errDB)
}

func TestTechIconService_List(t *testing.T) {
	m := newMocks(t)
	admin := model.User{Base: model.Base{ID: uuid.New()}, Username: "admin"}
	gone := uuid.New()
	at := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	m.techIcon.EXPECT().FindAll(ctx).Return([]model.TechIcon{
		{Tech: "redis", UpdatedBy: &admin.ID, UpdatedAt: at}, {Tech: "traefik", UpdatedBy: &gone, UpdatedAt: at}, {Tech: "zabbix"},
	}, nil)
	m.user.EXPECT().FindAll(ctx).Return([]model.User{admin}, nil)
	got, err := NewTechIconService(appContext.TestContext(nil), m.tx).List(ctx)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "admin", *got[0].UpdatedByName)
	assert.Nil(t, got[1].UpdatedByName, "the user was deleted")
	assert.Nil(t, got[2].UpdatedByName)
}

func TestTechIconService_GetResetTechnologies(t *testing.T) {
	m := newMocks(t)
	svc := NewTechIconService(appContext.TestContext(nil), m.tx)

	m.techIcon.EXPECT().Find(ctx, "traefik").Return(&model.TechIcon{Tech: "traefik"}, nil)
	got, err := svc.Get(ctx, "Traefik")
	require.NoError(t, err)
	assert.Equal(t, "traefik", got.Tech)
	m.techIcon.EXPECT().Find(ctx, "redis").Return(nil, gorm.ErrRecordNotFound)
	_, err = svc.Get(ctx, "redis")
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = svc.Get(ctx, "a/b")
	assert.ErrorIs(t, err, ErrInvalid)

	m.techIcon.EXPECT().Delete(ctx, "traefik").Return(nil)
	assert.NoError(t, svc.Reset(ctx, "TRAEFIK"))
	assert.ErrorIs(t, svc.Reset(ctx, ""), ErrInvalid)

	m.techIcon.EXPECT().FindTechnologies(ctx).Return([]string{"nginx", "redis"}, nil)
	techs, err := svc.Technologies(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"nginx", "redis"}, techs)
}
