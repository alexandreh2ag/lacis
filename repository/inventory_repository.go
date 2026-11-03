package repository

import (
	"context"
	"strings"
	"time"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type InventoryRepository interface {
	WithTx(tx *gorm.DB) InventoryRepository
	// Load returns the current content of the namespace, removed rows left
	// out, with its links.
	Load(ctx context.Context, namespaceID uuid.UUID) (*model.Inventory, error)
	// PurgeRemoved deletes for good the runtimes, services, webservers, apps,
	// containers and endpoints removed before before, and returns how many.
	PurgeRemoved(ctx context.Context, before time.Time) (int64, error)
}

type inventoryRepository struct{ db *gorm.DB }

func NewInventoryRepository(db *gorm.DB) InventoryRepository { return &inventoryRepository{db: db} }

func (r *inventoryRepository) WithTx(tx *gorm.DB) InventoryRepository {
	return &inventoryRepository{db: tx}
}

func (r *inventoryRepository) Load(ctx context.Context, namespaceID uuid.UUID) (*model.Inventory, error) {
	db := r.db.WithContext(ctx)
	inv := &model.Inventory{}
	if err := db.First(&inv.Namespace, "id = ?", namespaceID).Error; err != nil {
		return nil, err
	}
	ns := "namespace_id = ?"
	active := "namespace_id = ? and removed_at is null"
	hostIDs := db.Model(&model.Host{}).Select("id").Where(ns, namespaceID)
	appIDs := db.Model(&model.App{}).Select("id").Where(active, namespaceID)
	steps := []*gorm.DB{
		db.Where(ns, namespaceID).Order("slug").Find(&inv.Networks),
		db.Where(ns, namespaceID).Order("cidr").Find(&inv.CIDRs),
		db.Where("host_id in (?)", hostIDs).Order("host_id, from_network_id").Find(&inv.HostNetworks),
		db.Where(ns, namespaceID).Order("slug").Find(&inv.Projects),
		db.Where(ns, namespaceID).Order("sort_order, slug").Find(&inv.Environments),
		db.Where(ns, namespaceID).Order("hostname").Find(&inv.Hosts),
		db.Where("host_id in (?)", hostIDs).Order("host_id, ip").Find(&inv.IPs),
		db.Where(active, namespaceID).Order("host_id, type, path").Find(&inv.Runtimes),
		db.Where(active, namespaceID).Order("host_id, name").Find(&inv.Containers),
		db.Where(active, namespaceID).Order("host_id, type, instance").Find(&inv.Services),
		db.Where(active, namespaceID).Order("host_id, type").Find(&inv.Webservers),
		db.Where(active, namespaceID).Order("host_id, path").Find(&inv.Apps),
		db.Where("app_id in (?)", appIDs).Order("app_id, key, id").Find(&inv.Dependencies),
		db.Where(active, namespaceID).Order("host_id, kind, address, port").Find(&inv.Endpoints),
		db.Where(ns, namespaceID).Order("from_type, from_id, to_type, to_id, external_target").Find(&inv.Links),
	}
	for _, step := range steps {
		if step.Error != nil {
			return nil, step.Error
		}
	}
	return inv, nil
}

type LinkRepository interface {
	WithTx(tx *gorm.DB) LinkRepository
	// Replace makes links the links of the namespace: the links carry
	// stable ids, the ones already stored are updated in place, the others
	// inserted, and the stored ones absent from links deleted.
	Replace(ctx context.Context, namespaceID uuid.UUID, links []model.Link) error
}

type linkRepository struct{ db *gorm.DB }

func NewLinkRepository(db *gorm.DB) LinkRepository { return &linkRepository{db: db} }

func (r *linkRepository) WithTx(tx *gorm.DB) LinkRepository { return &linkRepository{db: tx} }

func (r *linkRepository) Replace(ctx context.Context, namespaceID uuid.UUID, links []model.Link) error {
	db := r.db.WithContext(ctx)
	ids := make([]string, 0, len(links))
	for _, l := range links {
		ids = append(ids, l.ID.String())
	}
	// One array parameter, whatever the number of links.
	keep := "{" + strings.Join(ids, ",") + "}"
	if err := db.Where("namespace_id = ? and id <> all(?::uuid[])", namespaceID, keep).Delete(&model.Link{}).Error; err != nil {
		return err
	}
	if len(links) == 0 {
		return nil
	}
	upsert := clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"namespace_id", "from_type", "from_id", "to_type", "to_id",
			"external_target", "source", "confidence", "detail", "computed_at"}),
	}
	return db.Clauses(upsert).CreateInBatches(&links, 500).Error
}

func (r *inventoryRepository) PurgeRemoved(ctx context.Context, before time.Time) (int64, error) {
	var total int64
	// Endpoints first: they name their owner without a foreign key.
	for _, table := range []string{"endpoints", "apps", "webservers", "services", "runtimes", "containers"} {
		res := r.db.WithContext(ctx).Exec("delete from "+table+" where removed_at < ?", before)
		if res.Error != nil {
			return total, res.Error
		}
		total += res.RowsAffected
	}
	return total, nil
}
