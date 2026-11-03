package model

import (
	"database/sql/driver"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

// Base gives an entity a UUID v7 primary key, generated before insertion
// when it is not set.
type Base struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey"`
}

func (b *Base) BeforeCreate(*gorm.DB) error {
	if b.ID != uuid.Nil {
		return nil
	}
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	b.ID = id
	return nil
}

// Interval maps a Postgres interval. Months count as 30 days.
type Interval time.Duration

func (i Interval) Duration() time.Duration {
	return time.Duration(i)
}

func (i Interval) Value() (driver.Value, error) {
	return fmt.Sprintf("%d microseconds", time.Duration(i).Microseconds()), nil
}

func (i *Interval) Scan(src any) error {
	if b, ok := src.([]byte); ok {
		src = string(b)
	}
	var pg pgtype.Interval
	if err := pg.Scan(src); err != nil {
		return fmt.Errorf("scan interval: %w", err)
	}
	if !pg.Valid {
		*i = 0
		return nil
	}
	days := int64(pg.Months)*30 + int64(pg.Days)
	*i = Interval(time.Duration(days)*24*time.Hour + time.Duration(pg.Microseconds)*time.Microsecond)
	return nil
}

// IP maps a Postgres inet holding a single address.
type IP struct {
	netip.Addr
}

func (ip IP) Value() (driver.Value, error) {
	if !ip.IsValid() {
		return nil, nil
	}
	return ip.String(), nil
}

func (ip *IP) Scan(src any) error {
	s, err := textValue(src)
	if err != nil || s == "" {
		ip.Addr = netip.Addr{}
		return err
	}
	// Postgres prints a host address without its prefix length, but
	// accepts one on input.
	if p, perr := netip.ParsePrefix(s); perr == nil {
		ip.Addr = p.Addr()
		return nil
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return fmt.Errorf("scan inet: %w", err)
	}
	ip.Addr = addr
	return nil
}

// CIDR maps a Postgres cidr.
type CIDR struct {
	netip.Prefix
}

func (c CIDR) Value() (driver.Value, error) {
	if !c.IsValid() {
		return nil, nil
	}
	return c.String(), nil
}

func (c *CIDR) Scan(src any) error {
	s, err := textValue(src)
	if err != nil || s == "" {
		c.Prefix = netip.Prefix{}
		return err
	}
	if !strings.Contains(s, "/") {
		return fmt.Errorf("scan cidr: %q has no prefix length", s)
	}
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return fmt.Errorf("scan cidr: %w", err)
	}
	c.Prefix = p
	return nil
}

func textValue(src any) (string, error) {
	switch v := src.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	default:
		return "", fmt.Errorf("unsupported type %T", src)
	}
}

// StringArray maps a Postgres text[].
type StringArray []string

func (a StringArray) Value() (driver.Value, error) {
	if a == nil {
		return "{}", nil
	}
	quoted := make([]string, len(a))
	for i, s := range a {
		quoted[i] = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
	}
	return "{" + strings.Join(quoted, ",") + "}", nil
}

func (a *StringArray) Scan(src any) error {
	s, err := textValue(src)
	if err != nil {
		return err
	}
	var arr pgtype.FlatArray[string]
	if err := pgtype.NewMap().SQLScanner(&arr).Scan(s); err != nil {
		return fmt.Errorf("scan text[]: %w", err)
	}
	*a = StringArray(arr)
	return nil
}
