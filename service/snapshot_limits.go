package service

import (
	"fmt"
	"reflect"
	"strings"
)

const (
	// MaxSnapshotString bounds each text of a snapshot, in bytes: names,
	// paths, rules and versions are far shorter.
	MaxSnapshotString = 8 << 10
	// MaxSnapshotItems bounds each list and each map of a snapshot.
	MaxSnapshotItems = 10000
)

// checkSizes returns ErrInvalid when a text, a list or a map of v is over
// MaxSnapshotString or MaxSnapshotItems, naming the first one found.
func checkSizes(v any) error {
	return walkSizes(reflect.ValueOf(v), "")
}

func walkSizes(v reflect.Value, path string) error {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return walkSizes(v.Elem(), path)
	case reflect.String:
		if v.Len() > MaxSnapshotString {
			return fmt.Errorf("%w: %s holds %d bytes, %d at most", ErrInvalid, sizePath(path), v.Len(), MaxSnapshotString)
		}
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 {
			// Raw JSON or bytes: bounded by the body as a whole.
			return nil
		}
		if v.Len() > MaxSnapshotItems {
			return fmt.Errorf("%w: %s holds %d items, %d at most", ErrInvalid, sizePath(path), v.Len(), MaxSnapshotItems)
		}
		for i := range v.Len() {
			if err := walkSizes(v.Index(i), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case reflect.Map:
		if v.Len() > MaxSnapshotItems {
			return fmt.Errorf("%w: %s holds %d items, %d at most", ErrInvalid, sizePath(path), v.Len(), MaxSnapshotItems)
		}
		iter := v.MapRange()
		for iter.Next() {
			key := fmt.Sprint(iter.Key().Interface())
			if err := walkSizes(iter.Key(), path+".<key>"); err != nil {
				return err
			}
			if err := walkSizes(iter.Value(), path+"."+key); err != nil {
				return err
			}
		}
	case reflect.Struct:
		t := v.Type()
		for i := range t.NumField() {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			field := strings.Split(f.Tag.Get("json"), ",")[0]
			if field == "-" {
				continue
			}
			if field == "" {
				field = f.Name
			}
			sub := path + "." + field
			if f.Anonymous && f.Tag.Get("json") == "" {
				sub = path
			}
			if err := walkSizes(v.Field(i), sub); err != nil {
				return err
			}
		}
	}
	return nil
}

// sizePath is a path for a message: without its leading dot, "snapshot" when
// empty.
func sizePath(path string) string {
	if path == "" {
		return "snapshot"
	}
	return strings.TrimPrefix(path, ".")
}
