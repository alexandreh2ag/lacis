package model

import (
	"time"

	"github.com/google/uuid"
)

// OAuthClient is a client that registered itself to reach the MCP
// endpoint, e.g. Claude. Its ID is the client_id.
type OAuthClient struct {
	ID           string `gorm:"primaryKey"`
	Name         string
	RedirectURIs StringArray `gorm:"column:redirect_uris;type:text[];not null"`
	// SecretHash is nil for a public client.
	SecretHash   *string
	CreatedAt    time.Time
	AuthorizedAt *time.Time
}

func (OAuthClient) TableName() string { return "oauth_clients" }

// OAuthCode is an authorization code; ID is the hash of the code.
type OAuthCode struct {
	ID            string `gorm:"primaryKey"`
	ClientID      string
	UserID        uuid.UUID   `gorm:"type:uuid"`
	OIDCGroups    StringArray `gorm:"column:oidc_groups;type:text[];not null"`
	RedirectURI   string
	CodeChallenge string
	Scope         string
	ExpiresAt     time.Time
}

func (OAuthCode) TableName() string { return "oauth_codes" }

// OAuthGrant is an access a user gave a client, with the hashes of its
// current tokens.
type OAuthGrant struct {
	Base
	ClientID            string
	UserID              uuid.UUID   `gorm:"type:uuid"`
	OIDCGroups          StringArray `gorm:"column:oidc_groups;type:text[];not null"`
	Scope               string
	AccessHash          string
	AccessExpiresAt     time.Time
	RefreshHash         string
	PreviousRefreshHash *string
	CreatedAt           time.Time
	LastUsedAt          time.Time
	ExpiresAt           time.Time
}

func (OAuthGrant) TableName() string { return "oauth_grants" }

// OAuthGrantView is a grant with the name of its client, as its user sees
// it.
type OAuthGrantView struct {
	OAuthGrant
	ClientName string
}
