package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// maxGroup bounds the name of an OIDC group.
const maxGroup = 200

// roleCodePattern is a role code: lowercase letters, digits and . _ -.
var roleCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// RolePermissions are the permissions of a role; their ids and role ids
// are ignored.
type RolePermissions struct {
	Namespace []model.NamespacePermission
	Global    []model.GlobalPermission
}

type RoleInput struct {
	Code        string
	Description string
	// OIDCGroups give the role to their members.
	OIDCGroups []string
	// Permissions replace those of the role; nil keeps them. The admin
	// role keeps every permission: it takes nil only.
	Permissions *RolePermissions
}

// RoleDetail is a role, its permissions, the OIDC groups given it and how
// many users hold it.
type RoleDetail struct {
	model.Role
	Namespace  []NamespaceGrant
	Global     []model.GlobalPermission
	OIDCGroups []string
	Users      int
}

// NamespaceGrant is a permission on a namespace, nil for all.
type NamespaceGrant struct {
	model.NamespacePermission
	Namespace *model.Namespace
}

// Builtin tells the admin role, created by the migrations: it cannot be
// deleted nor lose a permission.
func (r RoleDetail) Builtin() bool {
	return r.Code == model.RoleAdmin
}

type RoleService interface {
	// List returns the roles by code.
	List(ctx context.Context) ([]RoleDetail, error)
	Get(ctx context.Context, id uuid.UUID) (*RoleDetail, error)
	Create(ctx context.Context, in RoleInput) (*RoleDetail, error)
	Update(ctx context.Context, id uuid.UUID, in RoleInput) (*RoleDetail, error)
	// Delete removes a role but the admin role, and takes it from its users.
	Delete(ctx context.Context, id uuid.UUID) error
}

type roleService struct {
	appCtx *appContext.Context
	tx     repository.Transactor
}

func NewRoleService(appCtx *appContext.Context, tx repository.Transactor) RoleService {
	return &roleService{appCtx: appCtx, tx: tx}
}

func (s *roleService) List(ctx context.Context) ([]RoleDetail, error) {
	var out []RoleDetail
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		var err error
		out, err = listRoles(ctx, repos)
		return err
	})
	return out, err
}

func (s *roleService) Get(ctx context.Context, id uuid.UUID) (*RoleDetail, error) {
	var out *RoleDetail
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		var err error
		out, err = getRole(ctx, repos, id)
		return err
	})
	return out, err
}

func listRoles(ctx context.Context, repos *repository.Repositories) ([]RoleDetail, error) {
	roles, err := repos.Role.FindAll(ctx)
	if err != nil {
		return nil, err
	}
	ns, global, groups, err := repos.Role.FindGrants(ctx)
	if err != nil {
		return nil, err
	}
	held, err := repos.Role.FindUserRoles(ctx)
	if err != nil {
		return nil, err
	}
	namespaces, err := repos.Namespace.FindAll(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]RoleDetail, 0, len(roles))
	index := make(map[uuid.UUID]int, len(roles))
	for i, r := range roles {
		index[r.ID] = i
		out = append(out, RoleDetail{Role: r, Namespace: []NamespaceGrant{}, Global: []model.GlobalPermission{}, OIDCGroups: []string{}})
	}
	for _, p := range ns {
		i, ok := index[p.RoleID]
		if !ok {
			continue
		}
		grant := NamespaceGrant{NamespacePermission: p}
		if p.NamespaceID != nil {
			if j := slices.IndexFunc(namespaces, func(n model.Namespace) bool { return n.ID == *p.NamespaceID }); j >= 0 {
				grant.Namespace = &namespaces[j]
			}
		}
		out[i].Namespace = append(out[i].Namespace, grant)
	}
	for _, p := range global {
		if i, ok := index[p.RoleID]; ok {
			out[i].Global = append(out[i].Global, p)
		}
	}
	for _, g := range groups {
		if i, ok := index[g.RoleID]; ok {
			out[i].OIDCGroups = append(out[i].OIDCGroups, g.GroupName)
		}
	}
	for _, h := range held {
		if i, ok := index[h.RoleID]; ok {
			out[i].Users++
		}
	}
	return out, nil
}

func getRole(ctx context.Context, repos *repository.Repositories, id uuid.UUID) (*RoleDetail, error) {
	roles, err := listRoles(ctx, repos)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(roles, func(r RoleDetail) bool { return r.ID == id })
	if i < 0 {
		return nil, fmt.Errorf("%w: role %s", ErrNotFound, id)
	}
	return &roles[i], nil
}

func (s *roleService) Create(ctx context.Context, in RoleInput) (*RoleDetail, error) {
	var out *RoleDetail
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		role := &model.Role{}
		if err := setRole(role, in); err != nil {
			return err
		}
		groups, err := oidcGroups(in.OIDCGroups)
		if err != nil {
			return err
		}
		perms := in.Permissions
		if perms == nil {
			perms = &RolePermissions{}
		}
		if err = checkPermissions(ctx, repos, perms); err != nil {
			return err
		}
		if err = repos.Role.Create(ctx, role); err != nil {
			return duplicateRole(err, role.Code)
		}
		if err = repos.Role.SetPermissions(ctx, role.ID, perms.Namespace, perms.Global); err != nil {
			return err
		}
		if err = repos.Role.SetOIDCGroups(ctx, role.ID, groups); err != nil {
			return err
		}
		out, err = getRole(ctx, repos, role.ID)
		return err
	})
	return out, err
}

func (s *roleService) Update(ctx context.Context, id uuid.UUID, in RoleInput) (*RoleDetail, error) {
	var out *RoleDetail
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		role, err := found(repos.Role.FindByID(ctx, id))
		if err != nil {
			return err
		}
		if role.Code == model.RoleAdmin {
			if strings.TrimSpace(in.Code) != model.RoleAdmin {
				return fmt.Errorf("%w: the admin role keeps its code", ErrInvalid)
			}
			if in.Permissions != nil {
				return fmt.Errorf("%w: the admin role keeps every permission", ErrInvalid)
			}
		}
		if err = setRole(role, in); err != nil {
			return err
		}
		groups, err := oidcGroups(in.OIDCGroups)
		if err != nil {
			return err
		}
		if in.Permissions != nil {
			if err = checkPermissions(ctx, repos, in.Permissions); err != nil {
				return err
			}
		}
		if err = repos.Role.Update(ctx, role); err != nil {
			return duplicateRole(err, role.Code)
		}
		if in.Permissions != nil {
			if err = repos.Role.SetPermissions(ctx, id, in.Permissions.Namespace, in.Permissions.Global); err != nil {
				return err
			}
		}
		if err = repos.Role.SetOIDCGroups(ctx, id, groups); err != nil {
			return err
		}
		out, err = getRole(ctx, repos, id)
		return err
	})
	return out, err
}

func (s *roleService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		role, err := found(repos.Role.FindByID(ctx, id))
		if err != nil {
			return err
		}
		if role.Code == model.RoleAdmin {
			return fmt.Errorf("%w: the admin role cannot be deleted", ErrInvalid)
		}
		return repos.Role.Delete(ctx, id)
	})
}

// setRole checks and sets the code and the description of the role.
func setRole(role *model.Role, in RoleInput) error {
	code := strings.TrimSpace(in.Code)
	if !roleCodePattern.MatchString(code) {
		return fmt.Errorf("%w: code %q must hold 1 to 63 lowercase letters, digits or . _ -, starting with a letter or a digit", ErrInvalid, in.Code)
	}
	role.Code = code
	description := in.Description
	return setText(&role.Description, &description, "description", maxDescription)
}

// oidcGroups trims, sorts and dedupes the groups.
func oidcGroups(raw []string) ([]string, error) {
	groups := make([]string, 0, len(raw))
	for _, g := range raw {
		g = strings.TrimSpace(g)
		if g == "" || utf8.RuneCountInString(g) > maxGroup {
			return nil, fmt.Errorf("%w: an OIDC group holds 1 to %d characters", ErrInvalid, maxGroup)
		}
		groups = append(groups, g)
	}
	slices.Sort(groups)
	return slices.Compact(groups), nil
}

// checkPermissions checks the resources, the actions and the namespaces of
// the permissions, drops their duplicates and their ids.
func checkPermissions(ctx context.Context, repos *repository.Repositories, perms *RolePermissions) error {
	actions := []model.Action{model.ActionRead, model.ActionWrite, model.ActionAll}
	ns := make([]model.NamespacePermission, 0, len(perms.Namespace))
	for _, p := range perms.Namespace {
		if !slices.Contains([]model.Resource{model.ResourceInventory, model.ResourceMetadata, model.ResourceAll}, p.Resource) {
			return fmt.Errorf("%w: %q is no resource of a namespace", ErrInvalid, p.Resource)
		}
		if !slices.Contains(actions, p.Action) {
			return fmt.Errorf("%w: %q is no action", ErrInvalid, p.Action)
		}
		grant := model.NamespacePermission{NamespaceID: p.NamespaceID, Resource: p.Resource, Action: p.Action}
		if slices.ContainsFunc(ns, func(q model.NamespacePermission) bool {
			return q.Resource == grant.Resource && q.Action == grant.Action && equalID(q.NamespaceID, grant.NamespaceID)
		}) {
			continue
		}
		if p.NamespaceID != nil {
			if _, err := repos.Namespace.FindByID(ctx, *p.NamespaceID); errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: namespace %s", ErrNotFound, p.NamespaceID)
			} else if err != nil {
				return err
			}
		}
		ns = append(ns, grant)
	}
	global := make([]model.GlobalPermission, 0, len(perms.Global))
	for _, p := range perms.Global {
		if !slices.Contains([]model.Resource{model.ResourceNamespaces, model.ResourceTokens, model.ResourceUsers, model.ResourceRoles, model.ResourceAll}, p.Resource) {
			return fmt.Errorf("%w: %q is no global resource", ErrInvalid, p.Resource)
		}
		if !slices.Contains(actions, p.Action) {
			return fmt.Errorf("%w: %q is no action", ErrInvalid, p.Action)
		}
		grant := model.GlobalPermission{Resource: p.Resource, Action: p.Action}
		if !slices.Contains(global, grant) {
			global = append(global, grant)
		}
	}
	perms.Namespace, perms.Global = ns, global
	return nil
}

func equalID(a, b *uuid.UUID) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func duplicateRole(err error, code string) error {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return fmt.Errorf("%w: role %s", ErrAlreadyExists, code)
	}
	return err
}
