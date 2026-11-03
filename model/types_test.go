package model

import (
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBase_BeforeCreate(t *testing.T) {
	b := &Base{}
	require.NoError(t, b.BeforeCreate(nil))
	assert.Equal(t, uuid.Version(7), b.ID.Version())

	id := uuid.New()
	b = &Base{ID: id}
	require.NoError(t, b.BeforeCreate(nil))
	assert.Equal(t, id, b.ID, "an ID already set is kept")
}

func TestInterval(t *testing.T) {
	v, err := Interval(36*time.Hour + 1500*time.Microsecond).Value()
	require.NoError(t, err)
	assert.Equal(t, "129600001500 microseconds", v)

	tests := []struct {
		name    string
		src     any
		want    time.Duration
		wantErr bool
	}{
		{name: "days", src: "7 days", want: 7 * 24 * time.Hour},
		{name: "days and time", src: "1 day 12:30:00", want: 36*time.Hour + 30*time.Minute},
		{name: "months", src: "1 mon", want: 30 * 24 * time.Hour},
		{name: "bytes", src: []byte("00:00:01.5"), want: 1500 * time.Millisecond},
		{name: "null", src: nil, want: 0},
		{name: "invalid", src: "soon", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i := Interval(time.Hour)
			err := i.Scan(tt.src)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, i.Duration())
		})
	}
}

func TestIP(t *testing.T) {
	v, err := IP{Addr: netip.MustParseAddr("10.0.0.1")}.Value()
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.1", v)
	v, err = IP{}.Value()
	require.NoError(t, err)
	assert.Nil(t, v)

	tests := []struct {
		name    string
		src     any
		want    netip.Addr
		wantErr bool
	}{
		{name: "ipv4", src: "10.0.0.1", want: netip.MustParseAddr("10.0.0.1")},
		{name: "ipv6 bytes", src: []byte("fd00::1"), want: netip.MustParseAddr("fd00::1")},
		{name: "with prefix length", src: "10.0.0.1/24", want: netip.MustParseAddr("10.0.0.1")},
		{name: "null", src: nil},
		{name: "invalid", src: "nope", wantErr: true},
		{name: "unsupported type", src: 42, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := IP{Addr: netip.MustParseAddr("192.0.2.1")}
			err := ip.Scan(tt.src)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, ip.Addr)
		})
	}
}

func TestCIDR(t *testing.T) {
	v, err := CIDR{Prefix: netip.MustParsePrefix("10.0.0.0/8")}.Value()
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.0/8", v)
	v, err = CIDR{}.Value()
	require.NoError(t, err)
	assert.Nil(t, v)

	tests := []struct {
		name    string
		src     any
		want    netip.Prefix
		wantErr bool
	}{
		{name: "ipv4", src: "10.0.0.0/8", want: netip.MustParsePrefix("10.0.0.0/8")},
		{name: "ipv6 bytes", src: []byte("fd00::/64"), want: netip.MustParsePrefix("fd00::/64")},
		{name: "null", src: nil},
		{name: "no prefix length", src: "10.0.0.0", wantErr: true},
		{name: "invalid", src: "10.0.0.0/99", wantErr: true},
		{name: "unsupported type", src: 3.14, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := CIDR{Prefix: netip.MustParsePrefix("192.0.2.0/24")}
			err := c.Scan(tt.src)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, c.Prefix)
		})
	}
}

func TestEnrollmentToken_Usable(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	before, after := now.Add(-time.Hour), now.Add(time.Hour)
	tests := []struct {
		name  string
		token EnrollmentToken
		want  bool
	}{
		{name: "no expiry", token: EnrollmentToken{}, want: true},
		{name: "not expired", token: EnrollmentToken{ExpiresAt: &after}, want: true},
		{name: "expired", token: EnrollmentToken{ExpiresAt: &before}, want: false},
		{name: "expires now", token: EnrollmentToken{ExpiresAt: &now}, want: false},
		{name: "revoked", token: EnrollmentToken{RevokedAt: &before}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.token.Usable(now))
		})
	}
}

func TestTableNames(t *testing.T) {
	tables := map[string]interface{ TableName() string }{
		"namespaces": Namespace{}, "networks": Network{}, "network_cidrs": NetworkCIDR{},
		"projects": Project{}, "environments": Environment{}, "enrollment_tokens": EnrollmentToken{},
		"hosts": Host{}, "host_ips": HostIP{}, "snapshots": Snapshot{}, "runtimes": Runtime{},
		"services": Service{}, "apps": App{}, "endpoints": Endpoint{}, "app_dependencies": AppDependency{},
		"links": Link{}, "change_events": ChangeEvent{}, "users": User{}, "roles": Role{}, "sessions": Session{},
		"user_roles": UserRole{}, "role_oidc_groups": RoleOIDCGroup{}, "namespace_permissions": NamespacePermission{},
		"global_permissions": GlobalPermission{},
	}
	for want, m := range tables {
		assert.Equal(t, want, m.TableName())
	}
}

func TestNamespacePermission_Allows(t *testing.T) {
	acme, beta := uuid.New(), uuid.New()
	tests := []struct {
		name       string
		permission NamespacePermission
		namespace  uuid.UUID
		resource   Resource
		action     Action
		want       bool
	}{
		{name: "exact", permission: NamespacePermission{NamespaceID: &acme, Resource: ResourceMetadata, Action: ActionWrite}, namespace: acme, resource: ResourceMetadata, action: ActionWrite, want: true},
		{name: "other namespace", permission: NamespacePermission{NamespaceID: &acme, Resource: ResourceAll, Action: ActionAll}, namespace: beta, resource: ResourceMetadata, action: ActionRead, want: false},
		{name: "every namespace", permission: NamespacePermission{Resource: ResourceInventory, Action: ActionRead}, namespace: beta, resource: ResourceInventory, action: ActionRead, want: true},
		{name: "any resource", permission: NamespacePermission{NamespaceID: &acme, Resource: ResourceAll, Action: ActionRead}, namespace: acme, resource: ResourceInventory, action: ActionRead, want: true},
		{name: "any action", permission: NamespacePermission{NamespaceID: &acme, Resource: ResourceMetadata, Action: ActionAll}, namespace: acme, resource: ResourceMetadata, action: ActionWrite, want: true},
		{name: "other resource", permission: NamespacePermission{NamespaceID: &acme, Resource: ResourceMetadata, Action: ActionAll}, namespace: acme, resource: ResourceInventory, action: ActionRead, want: false},
		{name: "write does not grant read", permission: NamespacePermission{NamespaceID: &acme, Resource: ResourceMetadata, Action: ActionWrite}, namespace: acme, resource: ResourceMetadata, action: ActionRead, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.permission.Allows(tt.namespace, tt.resource, tt.action))
		})
	}
}

func TestGlobalPermission_Allows(t *testing.T) {
	tests := []struct {
		name       string
		permission GlobalPermission
		resource   Resource
		action     Action
		want       bool
	}{
		{name: "exact", permission: GlobalPermission{Resource: ResourceUsers, Action: ActionWrite}, resource: ResourceUsers, action: ActionWrite, want: true},
		{name: "everything", permission: GlobalPermission{Resource: ResourceAll, Action: ActionAll}, resource: ResourceRoles, action: ActionWrite, want: true},
		{name: "other resource", permission: GlobalPermission{Resource: ResourceTokens, Action: ActionAll}, resource: ResourceUsers, action: ActionRead, want: false},
		{name: "other action", permission: GlobalPermission{Resource: ResourceUsers, Action: ActionRead}, resource: ResourceUsers, action: ActionWrite, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.permission.Allows(tt.resource, tt.action))
		})
	}
}

func TestStringArray(t *testing.T) {
	v, err := StringArray{"a", `b "c"`, `d\e`}.Value()
	require.NoError(t, err)
	assert.Equal(t, `{"a","b \"c\"","d\\e"}`, v)
	v, err = StringArray(nil).Value()
	require.NoError(t, err)
	assert.Equal(t, "{}", v)

	var got StringArray
	require.NoError(t, got.Scan([]byte(`{a,"b \"c\"","d\\e"}`)))
	assert.Equal(t, StringArray{"a", `b "c"`, `d\e`}, got)
	require.NoError(t, got.Scan("{}"))
	assert.Empty(t, got)
	assert.Error(t, got.Scan("not an array"))
	assert.Error(t, got.Scan(42))
}
