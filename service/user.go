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

const (
	// MinPasswordLength is the minimal length of a local password.
	MinPasswordLength = 8
	// MaxPasswordLength bounds a password, in characters: a login with a
	// longer one is refused before hashing.
	MaxPasswordLength = 1024
)

// usernamePattern is a lowercased username: letters, digits and . _ @ + -,
// starting with a letter or a digit, so that an email fits.
var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._@+-]{0,99}$`)

// Username lowercases and trims a username, and checks it.
func Username(raw string) (string, error) {
	username := strings.ToLower(strings.TrimSpace(raw))
	if !usernamePattern.MatchString(username) {
		return "", refuse(ErrInvalid, ReasonInvalidUsername, nil,
			"username %q must hold 1 to 100 letters, digits or . _ @ + -, starting with a letter or a digit", raw)
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
	// Password is the new password; an SSO user given one signs in both ways.
	Password *string
	// CurrentPassword is required to change one's own password.
	CurrentPassword *string
}

// Account is a user and the roles it holds.
type Account struct {
	model.User
	Roles []model.Role
}

type UserService interface {
	// Create creates a local user with the given roles. The username is
	// stored lowercased. ErrForbidden when a role grants a permission the
	// actor does not hold; a nil actor, the command line, gives any.
	Create(ctx context.Context, actor *auth.Principal, in CreateUserInput) (*model.User, error)
	// Bootstrap creates the admin username, unless a user holds the admin
	// role already or has this username: an existing user, its password
	// included, is left as is. It reports whether the user was created.
	Bootstrap(ctx context.Context, username, password string) (bool, error)
	// List returns the users by username, with their roles.
	List(ctx context.Context) ([]Account, error)
	Get(ctx context.Context, id uuid.UUID) (*Account, error)
	// Update changes the names, or the password of a user, an SSO one
	// included, who can then sign in both ways; a new password ends the
	// other sessions of the user than the actor's. Changing one's own
	// password takes the current one, when there is one. ErrForbidden
	// unless the actor holds every permission of the roles of the user:
	// only an admin changes an admin.
	Update(ctx context.Context, actor *auth.Principal, id uuid.UUID, in UpdateUserInput) (*Account, error)
	// UpdateProfile changes the names, or the password, of the actor
	// itself, whatever its rights: a new password takes the current one and
	// ends the other sessions of the actor. An SSO user without a password
	// gets none this way (ReasonSSOUser): an administrator gives the first.
	UpdateProfile(ctx context.Context, actor *auth.Principal, in UpdateUserInput) (*Account, error)
	// SetRoles gives the user exactly these roles; ErrLastAdmin when it
	// would take the admin role from the last user holding it,
	// ErrForbidden unless the actor holds every permission of the roles
	// the user holds and gets.
	SetRoles(ctx context.Context, actor *auth.Principal, id uuid.UUID, roleIDs []uuid.UUID) (*Account, error)
	// Delete removes a user but the actor; ErrLastAdmin for the last
	// user holding the admin role, ErrForbidden as for Update.
	Delete(ctx context.Context, actor *auth.Principal, id uuid.UUID) error
}

type userService struct {
	appCtx *appContext.Context
	tx     repository.Transactor
}

func NewUserService(appCtx *appContext.Context, tx repository.Transactor) UserService {
	return &userService{appCtx: appCtx, tx: tx}
}

func (s *userService) Create(ctx context.Context, actor *auth.Principal, in CreateUserInput) (*model.User, error) {
	username, err := Username(in.Username)
	if err != nil {
		return nil, err
	}
	user := &model.User{Username: username}
	if err = setNames(user, &in.FirstName, &in.LastName); err != nil {
		return nil, err
	}
	passwordHash, err := passwordHash(ctx, in.Password)
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
		ids := make([]uuid.UUID, 0, len(roles))
		for _, role := range roles {
			ids = append(ids, role.ID)
		}
		if err := canGrantRoles(ctx, repos, actor, ids); err != nil {
			return err
		}
		if err := repos.User.Create(ctx, user); err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return refuse(ErrAlreadyExists, ReasonUserExists, nil, "user %s", username)
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
		admin, err := repos.Role.LockByCode(ctx, model.RoleAdmin)
		if err != nil {
			return err
		}
		// Once an admin exists, the bootstrap one is not needed: deleted,
		// it does not come back at the next start.
		n, err := repos.Role.CountUsers(ctx, admin.ID)
		if err != nil || n > 0 {
			exists = n > 0
			return err
		}
		_, err = repos.User.FindByUsername(ctx, normalized)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		exists = err == nil
		return err
	})
	if err != nil || exists {
		return false, err
	}
	_, err = s.Create(ctx, nil, CreateUserInput{Username: username, Password: password, Roles: []string{model.RoleAdmin}})
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
	return s.update(ctx, actor, id, in, true)
}

func (s *userService) UpdateProfile(ctx context.Context, actor *auth.Principal, in UpdateUserInput) (*Account, error) {
	if actor == nil {
		return nil, ErrUnauthorized
	}
	return s.update(ctx, actor, actor.User.ID, in, false)
}

// update changes the user of the id; manage checks first that the actor
// may manage it, which one's own profile does not need.
func (s *userService) update(ctx context.Context, actor *auth.Principal, id uuid.UUID, in UpdateUserInput, manage bool) (*Account, error) {
	var account *Account
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		user, err := found(repos.User.FindByID(ctx, id))
		if err != nil {
			return err
		}
		if manage {
			if err = canManage(ctx, repos, actor, id, nil); err != nil {
				return err
			}
		}
		if err = setNames(user, in.FirstName, in.LastName); err != nil {
			return err
		}
		if in.Password != nil {
			// An SSO user gets a first password from an administrator only;
			// then they change it as any user, given the current one.
			if user.PasswordHash == nil && !manage {
				return refuse(ErrInvalid, ReasonSSOUser, map[string]any{"username": user.Username},
					"%s has no password: only an administrator gives the first one", user.Username)
			}
			if actor != nil && actor.User.ID == id && user.PasswordHash != nil {
				if err = checkCurrent(ctx, in.CurrentPassword, *user.PasswordHash); err != nil {
					return err
				}
			}
			passwordHash, err := passwordHash(ctx, *in.Password)
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

func (s *userService) SetRoles(ctx context.Context, actor *auth.Principal, id uuid.UUID, roleIDs []uuid.UUID) (*Account, error) {
	var account *Account
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		if _, err := repos.Role.LockByCode(ctx, model.RoleAdmin); err != nil {
			return err
		}
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
		if err := canManage(ctx, repos, actor, id, ids); err != nil {
			return err
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
		return refuse(ErrInvalid, ReasonOwnAccount, nil, "you cannot delete your own account")
	}
	return s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		if _, err := repos.Role.LockByCode(ctx, model.RoleAdmin); err != nil {
			return err
		}
		if _, err := found(repos.User.FindByID(ctx, id)); err != nil {
			return err
		}
		if err := canManage(ctx, repos, actor, id, nil); err != nil {
			return err
		}
		if err := lastAdmin(ctx, repos, id); err != nil {
			return err
		}
		return repos.User.Delete(ctx, id)
	})
}

// canManage returns ErrForbidden unless the actor holds every permission
// of the roles the user holds and of the roles given: nobody takes over an
// account holding more than them, nor gives more than they hold.
func canManage(ctx context.Context, repos *repository.Repositories, actor *auth.Principal, userID uuid.UUID, given []uuid.UUID) error {
	if actor == nil {
		return nil
	}
	held, err := repos.Role.FindByUser(ctx, userID)
	if err != nil {
		return err
	}
	ids := slices.Clone(given)
	for _, r := range held {
		ids = append(ids, r.ID)
	}
	return canGrantRoles(ctx, repos, actor, ids)
}

// checkCurrent checks the current password given to change one's own.
func checkCurrent(ctx context.Context, current *string, encoded string) error {
	if current == nil || *current == "" {
		return refuse(ErrInvalid, ReasonCurrentPasswordRequired, nil, "the current password is required to change your own")
	}
	if utf8.RuneCountInString(*current) > MaxPasswordLength {
		return refuse(ErrInvalid, ReasonCurrentPasswordWrong, nil, "the current password is wrong")
	}
	ok, err := hash.VerifyPassword(ctx, *current, encoded)
	if err != nil {
		return err
	}
	if !ok {
		return refuse(ErrInvalid, ReasonCurrentPasswordWrong, nil, "the current password is wrong")
	}
	return nil
}

// lastAdmin returns ErrLastAdmin when the user is the last one holding the
// admin role, which it is about to lose. The caller locks the admin role
// first (LockByCode), so that two transactions cannot each take it from
// one of its last two holders.
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
			return refuse(ErrInvalid, ReasonNameTooLong, map[string]any{"max": maxName}, "a name holds %d characters at most", maxName)
		}
		*n.to = optional(name)
	}
	return nil
}

// checkPassword checks the length of a new password.
func checkPassword(password string) error {
	if len(password) < MinPasswordLength {
		return refuse(ErrInvalid, ReasonPasswordTooShort, map[string]any{"min": MinPasswordLength}, "the password needs at least %d characters", MinPasswordLength)
	}
	if utf8.RuneCountInString(password) > MaxPasswordLength {
		return refuse(ErrInvalid, ReasonPasswordTooLong, map[string]any{"max": MaxPasswordLength}, "the password holds %d characters at most", MaxPasswordLength)
	}
	return nil
}

// passwordHash checks the length of a password and hashes it.
func passwordHash(ctx context.Context, password string) (string, error) {
	if err := checkPassword(password); err != nil {
		return "", err
	}
	h, err := hash.Password(ctx, password)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return h, nil
}
