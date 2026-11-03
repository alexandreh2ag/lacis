package auth

import (
	"context"
	"testing"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestPrincipal(t *testing.T) {
	acme, other := uuid.New(), uuid.New()
	p := &Principal{
		Namespace: []model.NamespacePermission{
			{NamespaceID: &acme, Resource: model.ResourceInventory, Action: model.ActionAll},
			{NamespaceID: &other, Resource: model.ResourceMetadata, Action: model.ActionRead},
		},
		Global: []model.GlobalPermission{{Resource: model.ResourceTokens, Action: model.ActionRead}},
	}
	assert.True(t, p.Can(acme, model.ResourceInventory, model.ActionWrite))
	assert.False(t, p.Can(acme, model.ResourceMetadata, model.ActionRead))
	assert.False(t, p.Can(other, model.ResourceInventory, model.ActionRead))
	assert.True(t, p.CanGlobal(model.ResourceTokens, model.ActionRead))
	assert.False(t, p.CanGlobal(model.ResourceTokens, model.ActionWrite))
	assert.False(t, p.CanAnyNamespace(model.ResourceInventory, model.ActionRead))
	assert.Equal(t, []uuid.UUID{acme}, p.NamespaceIDs(model.ResourceInventory, model.ActionRead))

	admin := &Principal{Namespace: []model.NamespacePermission{{Resource: model.ResourceAll, Action: model.ActionAll}}}
	assert.True(t, admin.CanAnyNamespace(model.ResourceInventory, model.ActionRead))
	assert.True(t, admin.Can(other, model.ResourceMetadata, model.ActionWrite))
	assert.Empty(t, admin.NamespaceIDs(model.ResourceInventory, model.ActionRead))

	var anonymous *Principal
	assert.False(t, anonymous.Can(acme, model.ResourceInventory, model.ActionRead))
	assert.False(t, anonymous.CanGlobal(model.ResourceTokens, model.ActionRead))
	assert.False(t, anonymous.CanAnyNamespace(model.ResourceInventory, model.ActionRead))
	assert.Nil(t, anonymous.NamespaceIDs(model.ResourceInventory, model.ActionRead))
}

func TestContext(t *testing.T) {
	assert.Nil(t, FromContext(context.Background()))
	p := &Principal{SessionID: "s"}
	assert.Same(t, p, FromContext(WithPrincipal(context.Background(), p)))
}
