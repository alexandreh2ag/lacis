package service

import (
	"errors"
	"strings"
	"testing"

	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/hash"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gorm.io/gorm"
)

func TestUserService_Create(t *testing.T) {
	m := newMocks(t)
	admin := &model.Role{Base: model.Base{ID: uuid.New()}, Code: model.RoleAdmin}
	viewer := &model.Role{Base: model.Base{ID: uuid.New()}, Code: "acme-viewer"}
	userID := uuid.New()

	m.role.EXPECT().FindByCode(ctx, "admin").Return(admin, nil)
	m.role.EXPECT().FindByCode(ctx, "acme-viewer").Return(viewer, nil)
	m.user.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(func(_ any, u *model.User) error {
		u.ID = userID
		return nil
	})
	m.role.EXPECT().AssignToUser(ctx, userID, admin.ID).Return(nil)
	m.role.EXPECT().AssignToUser(ctx, userID, viewer.ID).Return(nil)

	svc := NewUserService(appContext.TestContext(nil), m.tx)
	user, err := svc.Create(ctx, CreateUserInput{
		Username:  "  Admin ",
		Password:  "correct horse",
		FirstName: " Ada ",
		LastName:  " Lovelace ",
		Roles:     []string{"admin", "acme-viewer"},
	})
	require.NoError(t, err)
	assert.Equal(t, "admin", user.Username)
	assert.Equal(t, "Ada", *user.FirstName)
	assert.Equal(t, "Lovelace", *user.LastName)
	assert.Equal(t, "Ada Lovelace", user.FullName())
	ok, err := hash.VerifyPassword("correct horse", *user.PasswordHash)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestUserService_Create_NoRoleNoName(t *testing.T) {
	m := newMocks(t)
	m.user.EXPECT().Create(ctx, gomock.Any()).Return(nil)

	user, err := NewUserService(appContext.TestContext(nil), m.tx).Create(ctx, CreateUserInput{Username: " Bob+Ops@Acme.example", Password: "12345678", LastName: " "})
	require.NoError(t, err)
	assert.Equal(t, "bob+ops@acme.example", user.Username, "an email is a username")
	assert.Nil(t, user.FirstName)
	assert.Nil(t, user.LastName)
	assert.Empty(t, user.FullName())
}

func TestUserService_Create_Errors(t *testing.T) {
	errDB := errors.New("db down")
	valid := CreateUserInput{Username: "bob", Password: "12345678", Roles: []string{"admin"}}
	admin := &model.Role{Base: model.Base{ID: uuid.New()}, Code: model.RoleAdmin}

	tests := []struct {
		name    string
		in      CreateUserInput
		setup   func(m *mocks)
		wantErr error
		wantMsg string
	}{
		{name: "empty username", in: CreateUserInput{Username: " ", Password: "12345678"}, wantErr: ErrInvalid, wantMsg: `username " "`},
		{name: "invalid username", in: CreateUserInput{Username: "bob martin", Password: "12345678"}, wantErr: ErrInvalid, wantMsg: `username "bob martin"`},
		{name: "username starting with a dot", in: CreateUserInput{Username: ".bob", Password: "12345678"}, wantErr: ErrInvalid, wantMsg: `username ".bob"`},
		{name: "username with a space", in: CreateUserInput{Username: "bob@acme example", Password: "12345678"}, wantErr: ErrInvalid, wantMsg: `username "bob@acme example"`},
		{name: "long name", in: CreateUserInput{Username: "bob", Password: "12345678", LastName: strings.Repeat("a", 101)}, wantErr: ErrInvalid, wantMsg: "100 characters"},
		{name: "short password", in: CreateUserInput{Username: "bob", Password: "1234567"}, wantErr: ErrInvalid, wantMsg: "8 characters"},
		{
			name:    "unknown role",
			in:      valid,
			setup:   func(m *mocks) { m.role.EXPECT().FindByCode(ctx, "admin").Return(nil, gorm.ErrRecordNotFound) },
			wantErr: ErrNotFound, wantMsg: `role "admin"`,
		},
		{
			name:    "role lookup fails",
			in:      valid,
			setup:   func(m *mocks) { m.role.EXPECT().FindByCode(ctx, "admin").Return(nil, errDB) },
			wantErr: errDB,
		},
		{
			name: "username taken",
			in:   valid,
			setup: func(m *mocks) {
				m.role.EXPECT().FindByCode(ctx, "admin").Return(admin, nil)
				m.user.EXPECT().Create(ctx, gomock.Any()).Return(gorm.ErrDuplicatedKey)
			},
			wantErr: ErrAlreadyExists, wantMsg: "user bob",
		},
		{
			name: "create fails",
			in:   valid,
			setup: func(m *mocks) {
				m.role.EXPECT().FindByCode(ctx, "admin").Return(admin, nil)
				m.user.EXPECT().Create(ctx, gomock.Any()).Return(errDB)
			},
			wantErr: errDB,
		},
		{
			name: "assign fails",
			in:   valid,
			setup: func(m *mocks) {
				m.role.EXPECT().FindByCode(ctx, "admin").Return(admin, nil)
				m.user.EXPECT().Create(ctx, gomock.Any()).Return(nil)
				m.role.EXPECT().AssignToUser(ctx, gomock.Any(), admin.ID).Return(errDB)
			},
			wantErr: errDB,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMocks(t)
			if tt.setup != nil {
				tt.setup(m)
			}
			_, err := NewUserService(appContext.TestContext(nil), m.tx).Create(ctx, tt.in)
			assert.ErrorIs(t, err, tt.wantErr)
			assert.ErrorContains(t, err, tt.wantMsg)
		})
	}
}

func TestUserService_Bootstrap(t *testing.T) {
	errDB := errors.New("db down")
	admin := &model.Role{Base: model.Base{ID: uuid.New()}, Code: model.RoleAdmin}
	tests := []struct {
		name        string
		createErr   error
		wantCreated bool
		wantErr     error
	}{
		{name: "created", wantCreated: true},
		{name: "already exists", createErr: gorm.ErrDuplicatedKey},
		{name: "fails", createErr: errDB, wantErr: errDB},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMocks(t)
			m.user.EXPECT().FindByUsername(ctx, "admin").Return(nil, gorm.ErrRecordNotFound)
			m.role.EXPECT().FindByCode(ctx, model.RoleAdmin).Return(admin, nil)
			m.user.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(func(_ any, u *model.User) error {
				assert.Equal(t, "admin", u.Username)
				return tt.createErr
			})
			if tt.createErr == nil {
				m.role.EXPECT().AssignToUser(ctx, gomock.Any(), admin.ID).Return(nil)
			}
			created, err := NewUserService(appContext.TestContext(nil), m.tx).Bootstrap(ctx, "Admin", "s3cret-password")
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, tt.wantCreated, created)
		})
	}
}

func TestUserService_Bootstrap_Exists(t *testing.T) {
	m := newMocks(t)
	m.user.EXPECT().FindByUsername(ctx, "admin").Return(&model.User{}, nil)
	created, err := NewUserService(appContext.TestContext(nil), m.tx).Bootstrap(ctx, " Admin", "s3cret-password")
	require.NoError(t, err)
	assert.False(t, created, "no insert, so no SQL error logged")

	m = newMocks(t)
	m.user.EXPECT().FindByUsername(ctx, gomock.Any()).Return(nil, errors.New("db down"))
	_, err = NewUserService(appContext.TestContext(nil), m.tx).Bootstrap(ctx, "a", "s3cret-password")
	assert.EqualError(t, err, "db down")

	_, err = NewUserService(appContext.TestContext(nil), newMocks(t).tx).Bootstrap(ctx, "a b", "s3cret-password")
	assert.ErrorIs(t, err, ErrInvalid, "checked before any lookup")
}
