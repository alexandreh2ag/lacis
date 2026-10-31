package repository

import (
	"context"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RoleRepository interface {
	WithTx(tx *gorm.DB) RoleRepository
	Create(ctx context.Context, role *model.Role) error
	// Update saves the code and the description of role.
	Update(ctx context.Context, role *model.Role) error
	// Delete removes the role, its permissions, its OIDC groups and takes it
	// from its users.
	Delete(ctx context.Context, id uuid.UUID) error
	FindByID(ctx context.Context, id uuid.UUID) (*model.Role, error)
	FindByCode(ctx context.Context, code string) (*model.Role, error)
	FindAll(ctx context.Context) ([]model.Role, error)
	// AssignToUser gives the role to the user; giving it again is a no-op.
	AssignToUser(ctx context.Context, userID, roleID uuid.UUID) error
	// SetUserRoles gives the user exactly these roles.
	SetUserRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) error
	FindByUser(ctx context.Context, userID uuid.UUID) ([]model.Role, error)
	// FindUserRoles returns who holds which role.
	FindUserRoles(ctx context.Context) ([]model.UserRole, error)
	// CountUsers counts the users holding the role.
	CountUsers(ctx context.Context, roleID uuid.UUID) (int64, error)
	// FindGrants returns the permissions and the OIDC groups of every role.
	FindGrants(ctx context.Context) ([]model.NamespacePermission, []model.GlobalPermission, []model.RoleOIDCGroup, error)
	// SetPermissions replaces the permissions of the role.
	SetPermissions(ctx context.Context, roleID uuid.UUID, ns []model.NamespacePermission, global []model.GlobalPermission) error
	// SetOIDCGroups replaces the OIDC groups given the role.
	SetOIDCGroups(ctx context.Context, roleID uuid.UUID, groups []string) error
	// FindPermissions returns the permissions of the roles of the user and
	// of the roles given to these OIDC groups.
	FindPermissions(ctx context.Context, userID uuid.UUID, groups []string) ([]model.NamespacePermission, []model.GlobalPermission, error)
}

type roleRepository struct {
	db *gorm.DB
}

func NewRoleRepository(db *gorm.DB) RoleRepository {
	return &roleRepository{db: db}
}

func (r *roleRepository) WithTx(tx *gorm.DB) RoleRepository {
	return &roleRepository{db: tx}
}

func (r *roleRepository) Create(ctx context.Context, role *model.Role) error {
	return r.db.WithContext(ctx).Create(role).Error
}

func (r *roleRepository) Update(ctx context.Context, role *model.Role) error {
	return r.db.WithContext(ctx).Model(role).Select("code", "description").Updates(role).Error
}

func (r *roleRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&model.Role{}, "id = ?", id).Error
}

func (r *roleRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.Role, error) {
	return first[model.Role](ctx, r.db, "id = ?", id)
}

func (r *roleRepository) FindByCode(ctx context.Context, code string) (*model.Role, error) {
	return first[model.Role](ctx, r.db, "code = ?", code)
}

func (r *roleRepository) FindAll(ctx context.Context) ([]model.Role, error) {
	var roles []model.Role
	err := r.db.WithContext(ctx).Order("code").Find(&roles).Error
	return roles, err
}

func (r *roleRepository) AssignToUser(ctx context.Context, userID, roleID uuid.UUID) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).
		Create(&model.UserRole{UserID: userID, RoleID: roleID}).Error
}

func (r *roleRepository) FindByUser(ctx context.Context, userID uuid.UUID) ([]model.Role, error) {
	var roles []model.Role
	err := r.db.WithContext(ctx).
		Joins("join user_roles on user_roles.role_id = roles.id").
		Where("user_roles.user_id = ?", userID).
		Order("roles.code").Find(&roles).Error
	return roles, err
}

func (r *roleRepository) FindPermissions(ctx context.Context, userID uuid.UUID, groups []string) ([]model.NamespacePermission, []model.GlobalPermission, error) {
	db := r.db.WithContext(ctx)
	roles := db.Model(&model.UserRole{}).Select("role_id").Where("user_id = ?", userID)
	if len(groups) > 0 {
		roles = db.Raw("? union ?", roles, db.Model(&model.RoleOIDCGroup{}).Select("role_id").Where("group_name in ?", groups))
	}
	var ns []model.NamespacePermission
	if err := db.Where("role_id in (?)", roles).Order("id").Find(&ns).Error; err != nil {
		return nil, nil, err
	}
	var global []model.GlobalPermission
	if err := db.Where("role_id in (?)", roles).Order("id").Find(&global).Error; err != nil {
		return nil, nil, err
	}
	return ns, global, nil
}

func (r *roleRepository) SetUserRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) error {
	db := r.db.WithContext(ctx)
	if err := db.Delete(&model.UserRole{}, "user_id = ?", userID).Error; err != nil {
		return err
	}
	if len(roleIDs) == 0 {
		return nil
	}
	rows := make([]model.UserRole, 0, len(roleIDs))
	for _, id := range roleIDs {
		rows = append(rows, model.UserRole{UserID: userID, RoleID: id})
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

func (r *roleRepository) FindUserRoles(ctx context.Context) ([]model.UserRole, error) {
	var rows []model.UserRole
	err := r.db.WithContext(ctx).Order("user_id, role_id").Find(&rows).Error
	return rows, err
}

func (r *roleRepository) CountUsers(ctx context.Context, roleID uuid.UUID) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.UserRole{}).Where("role_id = ?", roleID).Count(&n).Error
	return n, err
}

func (r *roleRepository) FindGrants(ctx context.Context) ([]model.NamespacePermission, []model.GlobalPermission, []model.RoleOIDCGroup, error) {
	db := r.db.WithContext(ctx)
	var ns []model.NamespacePermission
	if err := db.Order("role_id, id").Find(&ns).Error; err != nil {
		return nil, nil, nil, err
	}
	var global []model.GlobalPermission
	if err := db.Order("role_id, id").Find(&global).Error; err != nil {
		return nil, nil, nil, err
	}
	var groups []model.RoleOIDCGroup
	if err := db.Order("role_id, group_name").Find(&groups).Error; err != nil {
		return nil, nil, nil, err
	}
	return ns, global, groups, nil
}

func (r *roleRepository) SetPermissions(ctx context.Context, roleID uuid.UUID, ns []model.NamespacePermission, global []model.GlobalPermission) error {
	db := r.db.WithContext(ctx)
	if err := db.Delete(&model.NamespacePermission{}, "role_id = ?", roleID).Error; err != nil {
		return err
	}
	if err := db.Delete(&model.GlobalPermission{}, "role_id = ?", roleID).Error; err != nil {
		return err
	}
	for i := range ns {
		ns[i].RoleID = roleID
	}
	for i := range global {
		global[i].RoleID = roleID
	}
	if len(ns) > 0 {
		if err := db.Create(&ns).Error; err != nil {
			return err
		}
	}
	if len(global) > 0 {
		return db.Create(&global).Error
	}
	return nil
}

func (r *roleRepository) SetOIDCGroups(ctx context.Context, roleID uuid.UUID, groups []string) error {
	db := r.db.WithContext(ctx)
	if err := db.Delete(&model.RoleOIDCGroup{}, "role_id = ?", roleID).Error; err != nil {
		return err
	}
	if len(groups) == 0 {
		return nil
	}
	rows := make([]model.RoleOIDCGroup, 0, len(groups))
	for _, g := range groups {
		rows = append(rows, model.RoleOIDCGroup{RoleID: roleID, GroupName: g})
	}
	return db.Create(&rows).Error
}
