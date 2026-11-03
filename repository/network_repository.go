package repository

import (
	"context"
	"net/netip"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type NetworkRepository interface {
	WithTx(tx *gorm.DB) NetworkRepository
	Create(ctx context.Context, network *model.Network) error
	FindByID(ctx context.Context, id uuid.UUID) (*model.Network, error)
	FindBySlug(ctx context.Context, namespaceID uuid.UUID, slug string) (*model.Network, error)
	// FindByIP returns the network of the namespace whose most specific
	// CIDR holds ip, gorm.ErrRecordNotFound when none does.
	FindByIP(ctx context.Context, namespaceID uuid.UUID, ip netip.Addr) (*model.Network, error)
	// Ensure loads the network of that namespace and slug into network,
	// created from network when missing.
	Ensure(ctx context.Context, network *model.Network) error
	// AddCIDR adds a CIDR to a network; adding it again does nothing.
	AddCIDR(ctx context.Context, cidr *model.NetworkCIDR) error
	// Update saves every column of network.
	Update(ctx context.Context, network *model.Network) error
	// Delete removes a network; the hosts moved into it go back to the one
	// it was split from.
	Delete(ctx context.Context, id uuid.UUID) error
	// MoveHosts puts the hosts in the network to, split from the network
	// from their IPs give, or back in from itself.
	MoveHosts(ctx context.Context, from, to uuid.UUID, hostIDs []uuid.UUID) error
}

type networkRepository struct {
	db *gorm.DB
}

func NewNetworkRepository(db *gorm.DB) NetworkRepository {
	return &networkRepository{db: db}
}

func (r *networkRepository) WithTx(tx *gorm.DB) NetworkRepository {
	return &networkRepository{db: tx}
}

func (r *networkRepository) Update(ctx context.Context, network *model.Network) error {
	return r.db.WithContext(ctx).Save(network).Error
}

func (r *networkRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&model.Network{}, "id = ?", id).Error
}

func (r *networkRepository) MoveHosts(ctx context.Context, from, to uuid.UUID, hostIDs []uuid.UUID) error {
	if len(hostIDs) == 0 {
		return nil
	}
	db := r.db.WithContext(ctx)
	if from == to {
		return db.Delete(&model.HostNetwork{}, "from_network_id = ? and host_id in ?", from, hostIDs).Error
	}
	rows := make([]model.HostNetwork, 0, len(hostIDs))
	for _, id := range hostIDs {
		rows = append(rows, model.HostNetwork{HostID: id, FromNetworkID: from, NetworkID: to})
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "host_id"}, {Name: "from_network_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"network_id"}),
	}).Create(&rows).Error
}

func (r *networkRepository) Create(ctx context.Context, network *model.Network) error {
	return r.db.WithContext(ctx).Create(network).Error
}

func (r *networkRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.Network, error) {
	return first[model.Network](ctx, r.db, "id = ?", id)
}

func (r *networkRepository) FindBySlug(ctx context.Context, namespaceID uuid.UUID, slug string) (*model.Network, error) {
	return first[model.Network](ctx, r.db, "namespace_id = ? AND slug = ?", namespaceID, slug)
}

func (r *networkRepository) FindByIP(ctx context.Context, namespaceID uuid.UUID, ip netip.Addr) (*model.Network, error) {
	var network model.Network
	err := r.db.WithContext(ctx).
		Joins("join network_cidrs c on c.network_id = networks.id").
		Where("c.namespace_id = ? and c.cidr >>= ?::inet", namespaceID, ip.String()).
		Order("masklen(c.cidr) desc").
		First(&network).Error
	if err != nil {
		return nil, err
	}
	return &network, nil
}

func (r *networkRepository) Ensure(ctx context.Context, network *model.Network) error {
	return ensure(ctx, r.db, network, "namespace_id = ? and slug = ?", network.NamespaceID, network.Slug)
}

func (r *networkRepository) AddCIDR(ctx context.Context, cidr *model.NetworkCIDR) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(cidr).Error
}

// ensure inserts row unless a row matches query, then loads the row that
// matches into row: concurrent inserts of the same row do not fail.
func ensure[T any](ctx context.Context, db *gorm.DB, row *T, query string, args ...any) error {
	db = db.WithContext(ctx)
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error; err != nil {
		return err
	}
	// Into a fresh value: GORM would add the id generated for the insert,
	// which did nothing, to the conditions.
	var found T
	if err := db.Where(query, args...).First(&found).Error; err != nil {
		return err
	}
	*row = found
	return nil
}
