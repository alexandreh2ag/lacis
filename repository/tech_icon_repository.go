package repository

import (
	"context"

	"github.com/alexandreh2ag/lacis/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TechIconRepository interface {
	WithTx(tx *gorm.DB) TechIconRepository
	// Save stores the logo of the technology, replacing the one it has.
	Save(ctx context.Context, icon *model.TechIcon) error
	Find(ctx context.Context, tech string) (*model.TechIcon, error)
	// FindAll returns the logos by technology, without their content.
	FindAll(ctx context.Context) ([]model.TechIcon, error)
	// Delete removes the logo of the technology; none is a no-op.
	Delete(ctx context.Context, tech string) error
	// FindTechnologies returns the technologies the inventory holds, sorted:
	// the types of the services, webservers and runtimes, and the
	// frameworks of the apps, removed ones left out.
	FindTechnologies(ctx context.Context) ([]string, error)
}

type techIconRepository struct{ db *gorm.DB }

func NewTechIconRepository(db *gorm.DB) TechIconRepository { return &techIconRepository{db: db} }

func (r *techIconRepository) WithTx(tx *gorm.DB) TechIconRepository {
	return &techIconRepository{db: tx}
}

func (r *techIconRepository) Save(ctx context.Context, icon *model.TechIcon) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tech"}},
		DoUpdates: clause.AssignmentColumns([]string{"content_type", "content", "updated_by", "updated_at"}),
	}).Create(icon).Error
}

func (r *techIconRepository) Find(ctx context.Context, tech string) (*model.TechIcon, error) {
	return first[model.TechIcon](ctx, r.db, "tech = ?", tech)
}

func (r *techIconRepository) FindAll(ctx context.Context) ([]model.TechIcon, error) {
	var icons []model.TechIcon
	err := r.db.WithContext(ctx).Omit("content").Order("tech").Find(&icons).Error
	return icons, err
}

func (r *techIconRepository) Delete(ctx context.Context, tech string) error {
	return r.db.WithContext(ctx).Delete(&model.TechIcon{}, "tech = ?", tech).Error
}

func (r *techIconRepository) FindTechnologies(ctx context.Context) ([]string, error) {
	var techs []string
	err := r.db.WithContext(ctx).Raw(`
		select lower(type) from services where removed_at is null
		union select lower(type) from webservers where removed_at is null
		union select lower(type) from runtimes where removed_at is null
		union select lower(framework) from apps where removed_at is null and framework is not null and framework <> ''
		order by 1`).Scan(&techs).Error
	return techs, err
}
