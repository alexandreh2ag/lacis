package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/alexandreh2ag/lacis/auth"
	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/icon"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/google/uuid"
)

// techPattern is a technology as the agent names it, lowercased: redis,
// php-fpm, next.js.
var techPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]{0,62}$`)

// Tech lowercases and trims the name of a technology, and checks it.
func Tech(raw string) (string, error) {
	tech := strings.ToLower(strings.TrimSpace(raw))
	if !techPattern.MatchString(tech) {
		return "", fmt.Errorf("%w: technology %q must hold 1 to 63 letters, digits or . _ + -, starting with a letter or a digit", ErrInvalid, raw)
	}
	return tech, nil
}

// TechIconInfo is the logo of a technology without its content, and the
// username of who uploaded it, nil once deleted.
type TechIconInfo struct {
	model.TechIcon
	UpdatedByName *string
}

// TechIconService keeps the logos uploaded for technologies, checked
// against the icons configuration; the front shows them in place of the
// ones it embeds.
type TechIconService interface {
	List(ctx context.Context) ([]TechIconInfo, error)
	// Technologies returns the technologies the inventory holds.
	Technologies(ctx context.Context) ([]string, error)
	Get(ctx context.Context, tech string) (*model.TechIcon, error)
	// Set checks the file and makes it the logo of the technology; a file
	// refused is ErrInvalid, wrapping the reason of the icon package.
	Set(ctx context.Context, actor *auth.Principal, tech, name string, data []byte) (*TechIconInfo, error)
	// Reset removes the logo uploaded: the default one shows again.
	Reset(ctx context.Context, tech string) error
}

type techIconService struct {
	appCtx *appContext.Context
	tx     repository.Transactor
}

func NewTechIconService(appCtx *appContext.Context, tx repository.Transactor) TechIconService {
	return &techIconService{appCtx: appCtx, tx: tx}
}

func (s *techIconService) List(ctx context.Context) ([]TechIconInfo, error) {
	var out []TechIconInfo
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		icons, err := repos.TechIcon.FindAll(ctx)
		if err != nil {
			return err
		}
		users, err := repos.User.FindAll(ctx)
		if err != nil {
			return err
		}
		names := make(map[uuid.UUID]string, len(users))
		for _, u := range users {
			names[u.ID] = u.Username
		}
		out = make([]TechIconInfo, 0, len(icons))
		for _, i := range icons {
			info := TechIconInfo{TechIcon: i}
			if i.UpdatedBy != nil {
				if name, ok := names[*i.UpdatedBy]; ok {
					info.UpdatedByName = &name
				}
			}
			out = append(out, info)
		}
		return nil
	})
	return out, err
}

func (s *techIconService) Technologies(ctx context.Context) ([]string, error) {
	var out []string
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		var err error
		out, err = repos.TechIcon.FindTechnologies(ctx)
		return err
	})
	return out, err
}

func (s *techIconService) Get(ctx context.Context, raw string) (*model.TechIcon, error) {
	tech, err := Tech(raw)
	if err != nil {
		return nil, err
	}
	var out *model.TechIcon
	err = s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		var err error
		out, err = found(repos.TechIcon.Find(ctx, tech))
		return err
	})
	return out, err
}

func (s *techIconService) Set(ctx context.Context, actor *auth.Principal, raw, name string, data []byte) (*TechIconInfo, error) {
	tech, err := Tech(raw)
	if err != nil {
		return nil, err
	}
	cfg := s.appCtx.Config.Icons
	img, err := icon.Check(name, data, icon.Limits{MaxSize: int64(cfg.MaxSize), MinSide: cfg.MinSide, MaxSide: cfg.MaxSide})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	info := &TechIconInfo{TechIcon: model.TechIcon{Tech: tech, ContentType: img.ContentType, Content: data, UpdatedAt: s.appCtx.Clock.Now()}}
	if actor != nil {
		info.UpdatedBy, info.UpdatedByName = &actor.User.ID, &actor.User.Username
	}
	err = s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		return repos.TechIcon.Save(ctx, &info.TechIcon)
	})
	if err != nil {
		return nil, err
	}
	return info, nil
}

func (s *techIconService) Reset(ctx context.Context, raw string) error {
	tech, err := Tech(raw)
	if err != nil {
		return err
	}
	return s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		return repos.TechIcon.Delete(ctx, tech)
	})
}
