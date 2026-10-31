package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/alexandreh2ag/lacis/auth"
	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/hash"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MinPasswordLength is the minimal length of a local password.
const MinPasswordLength = 8

// usernamePattern is a lowercased username: letters, digits and . _ @ + -,
// starting with a letter or a digit, so that an email fits.
var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._@+-]{0,99}$`)

// Username lowercases and trims a username, and checks it.
func Username(raw string) (string, error) {
	username := strings.ToLower(strings.TrimSpace(raw))
	if !usernamePattern.MatchString(username) {
		return "", fmt.Errorf("%w: username %q must hold 1 to 100 letters, digits or . _ @ + -, starting with a letter or a digit",
			ErrInvalid, raw)
	}
	return username, nil
}

type CreateUserInput struct {
	Username  string
	Password  string
	FirstName string
	LastName  string
	// Roles are role codes, e.g. "admin".
	Roles []string
	// RoleIDs are roles given by id, on top of Roles.
	RoleIDs []uuid.UUID
}

// UpdateUserInput changes the fields set; a name set empty is cleared.
type UpdateUserInput struct {
	FirstName *string
	LastName  *string
	// Password is the new password of a local user.
	Password *string
}

// Account is a user and the roles it holds.
type Account struct {
	model.User
	Roles []model.Role
}

type UserService interface {
	// Create creates a local user with the given roles. The username is
	// stored lowercased.
	Create(ctx context.Context, in CreateUserInput) (*model.User, error)
	// Bootstrap creates the admin username, unless a user already has this
	// username: an existing user, its password included, is left as is. It
	// reports whether the user was created.
	Bootstrap(ctx context.Context, username, password string) (bool, error)
	// List returns the users by username, with their roles.
	List(ctx context.Context) ([]Account, error)
	Get(ctx context.Context, id uuid.UUID) (*Account, error)
	// Update changes the names, or the password of a local user; a new
	// password ends the other sessions of the user than the actor's.
	Update(ctx context.Context, actor *auth.Principal, id uuid.UUID, in UpdateUserInput) (*Account, error)
	// SetRoles gives the user exactly these roles; ErrLastAdmin when it
	// would take the admin role from the last user holding it.
	SetRoles(ctx context.Context, id uuid.UUID, roleIDs []uuid.UUID) (*Account, error)
	// Delete removes a user but the actor; ErrLastAdmin for the last
	// user holding the admin role.
	Delete(ctx context.Context, actor *auth.Principal, id uuid.UUID) error
}

type userService struct {
	appCtx *appContext.Context
	tx     repository.Transactor
}

func NewUserService(appCtx *appContext.Context, tx repository.Transactor) UserService {
	return &userService{appCtx: appCtx, tx: tx}
}

func (s *userService) Create(ctx context.Context, in CreateUserInput) (*model.User, error) {
	username, err := Username(in.Username)
	if err != nil {
		return nil, err
	}
	user := &model.User{Username: username}
	if err = setNames(user, &in.FirstName, &in.LastName); err != nil {
		return nil, err
	}
	passwordHash, err := passwordHash(in.Password)
	if err != nil {
		return nil, err
	}
	user.PasswordHash = &passwordHash

	err = s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		roles := make([]*model.Role, 0, len(in.Roles))
		for _, code := range in.Roles {
			role, err := repos.Role.FindByCode(ctx, code)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: role %q", ErrNotFound, code)
			}
			if err != nil {
				return err
			}
			roles = append(roles, role)
		}
		for _, id := range in.RoleIDs {
			role, err := found(repos.Role.FindByID(ctx, id))
			if err != nil {
				return fmt.Errorf("%w: role %s", ErrNotFound, id)
			}
			roles = append(roles, role)
		}
		if err := repos.User.Create(ctx, user); err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return fmt.Errorf("%w: user %s", ErrAlreadyExists, username)
			}
			return err
		}
		for _, role := range roles {
			if err := repos.Role.AssignToUser(ctx, user.ID, role.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *userService) Bootstrap(ctx context.Context, username, password string) (bool, error) {
	normalized, err := Username(username)
	if err != nil {
		return false, err
	}
	// Looked up first: a failed insert would be logged as an SQL error at
	// every start.
	exists := false
	err = s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		_, err := repos.User.FindByUsername(ctx, normalized)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		exists = err == nil
		return err
	})
	if err != nil || exists {
		return false, err
	}
	_, err = s.Create(ctx, CreateUserInput{Username: username, Password: password, Roles: []string{model.RoleAdmin}})
	if errors.Is(err, ErrAlreadyExists) {
		return false, nil
	}
	return err == nil, err
}

func (s *userService) List(ctx context.Context) ([]Account, error) {
	var accounts []Account
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		users, err := repos.User.FindAll(ctx)
		if err != nil {
			return err
		}
		roles, err := repos.Role.FindAll(ctx)
		if err != nil {
			return err
		}
		held, err := repos.Role.FindUserRoles(ctx)
		if err != nil {
			return err
		}
		byID := make(map[uuid.UUID]model.Role, len(roles))
		for _, r := range roles {
			byID[r.ID] = r
		}
		byUser := make(map[uuid.UUID][]model.Role)
		for _, h := range held {
			byUser[h.UserID] = append(byUser[h.UserID], byID[h.RoleID])
		}
		accounts = make([]Account, 0, len(users))
		for _, u := range users {
			userRoles := byUser[u.ID]
			slices.SortFunc(userRoles, func(a, b model.Role) int { return strings.Compare(a.Code, b.Code) })
			accounts = append(accounts, Account{User: u, Roles: userRoles})
		}
		return nil
	})
	return accounts, err
}

func (s *userService) Get(ctx context.Context, id uuid.UUID) (*Account, error) {
	var account *Account
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		var err error
		account, err = getAccount(ctx, repos, id)
		return err
	})
	return account, err
}

// getAccount returns the user of the id and its roles.
func getAccount(ctx context.Context, repos *repository.Repositories, id uuid.UUID) (*Account, error) {
	user, err := found(repos.User.FindByID(ctx, id))
	if err != nil {
		return nil, err
	}
	roles, err := repos.Role.FindByUser(ctx, id)
	if err != nil {
		return nil, err
	}
	return &Account{User: *user, Roles: roles}, nil
}

func (s *userService) Update(ctx context.Context, actor *auth.Principal, id uuid.UUID, in UpdateUserInput) (*Account, error) {
	var account *Account
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		user, err := found(repos.User.FindByID(ctx, id))
		if err != nil {
			return err
		}
		if err = setNames(user, in.FirstName, in.LastName); err != nil {
			return err
		}
		if in.Password != nil {
			if user.PasswordHash == nil {
				return fmt.Errorf("%w: %s signs in with SSO and has no password", ErrInvalid, user.Username)
			}
			passwordHash, err := passwordHash(*in.Password)
			if err != nil {
				return err
			}
			user.PasswordHash = &passwordHash
			keep := ""
			if actor != nil && actor.User.ID == id {
				keep = actor.SessionID
			}
			if err = repos.Session.DeleteByUser(ctx, id, keep); err != nil {
				return err
			}
		}
		if err = repos.User.Update(ctx, user); err != nil {
			return err
		}
		account, err = getAccount(ctx, repos, id)
		return err
	})
	return account, err
}

func (s *userService) SetRoles(ctx context.Context, id uuid.UUID, roleIDs []uuid.UUID) (*Account, error) {
	var account *Account
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		if _, err := found(repos.User.FindByID(ctx, id)); err != nil {
			return err
		}
		ids := make([]uuid.UUID, 0, len(roleIDs))
		keepsAdmin := false
		for _, roleID := range roleIDs {
			if slices.Contains(ids, roleID) {
				continue
			}
			role, err := found(repos.Role.FindByID(ctx, roleID))
			if err != nil {
				return fmt.Errorf("%w: role %s", ErrNotFound, roleID)
			}
			keepsAdmin = keepsAdmin || role.Code == model.RoleAdmin
			ids = append(ids, roleID)
		}
		if !keepsAdmin {
			if err := lastAdmin(ctx, repos, id); err != nil {
				return err
			}
		}
		if err := repos.Role.SetUserRoles(ctx, id, ids); err != nil {
			return err
		}
		var err error
		account, err = getAccount(ctx, repos, id)
		return err
	})
	return account, err
}

func (s *userService) Delete(ctx context.Context, actor *auth.Principal, id uuid.UUID) error {
	if actor != nil && actor.User.ID == id {
		return fmt.Errorf("%w: you cannot delete your own account", ErrInvalid)
	}
	return s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		if _, err := found(repos.User.FindByID(ctx, id)); err != nil {
			return err
		}
		if err := lastAdmin(ctx, repos, id); err != nil {
			return err
		}
		return repos.User.Delete(ctx, id)
	})
}

// lastAdmin returns ErrLastAdmin when the user is the last one holding the
// admin role, which it is about to lose.
func lastAdmin(ctx context.Context, repos *repository.Repositories, userID uuid.UUID) error {
	roles, err := repos.Role.FindByUser(ctx, userID)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(roles, func(r model.Role) bool { return r.Code == model.RoleAdmin })
	if i < 0 {
		return nil
	}
	n, err := repos.Role.CountUsers(ctx, roles[i].ID)
	if err != nil {
		return err
	}
	if n <= 1 {
		return ErrLastAdmin
	}
	return nil
}

// setNames sets the first and last names given, trimmed; empty clears.
func setNames(user *model.User, firstName, lastName *string) error {
	for _, n := range []struct {
		to   **string
		from *string
	}{{&user.FirstName, firstName}, {&user.LastName, lastName}} {
		if n.from == nil {
			continue
		}
		name := strings.TrimSpace(*n.from)
		if utf8.RuneCountInString(name) > maxName {
			return fmt.Errorf("%w: a name holds %d characters at most", ErrInvalid, maxName)
		}
		*n.to = optional(name)
	}
	return nil
}

// passwordHash checks the length of a password and hashes it.
func passwordHash(password string) (string, error) {
	if len(password) < MinPasswordLength {
		return "", fmt.Errorf("%w: the password needs at least %d characters", ErrInvalid, MinPasswordLength)
	}
	h, err := hash.Password(password)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return h, nil
}
