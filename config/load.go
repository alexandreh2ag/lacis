package config

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/afero"
	"github.com/spf13/viper"
)

// Extensions are the accepted configuration file extensions, by priority.
var Extensions = []string{".yaml", ".yml"}

// FindFile returns the configuration file designated by path: path itself
// when it is a file, otherwise <path>/<name>.yaml or <path>/<name>.yml. When
// both exist, the .yaml file wins and the other one is returned in ignored.
// file is empty when no file exists, which is valid: in Docker the
// configuration usually comes from the environment only.
func FindFile(fs afero.Fs, path, name string) (file string, ignored []string, err error) {
	if path == "" {
		path = DefaultDir
	}
	info, err := fs.Stat(path)
	if err != nil {
		if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
			return "", nil, fmt.Errorf("config file %s: %w", path, err)
		}
		return "", nil, nil
	}
	if !info.IsDir() {
		return path, nil, nil
	}
	for _, ext := range Extensions {
		candidate := filepath.Join(path, name+ext)
		if _, err := fs.Stat(candidate); err != nil {
			continue
		}
		if file == "" {
			file = candidate
		} else {
			ignored = append(ignored, candidate)
		}
	}
	return file, ignored, nil
}

// Load reads the configuration file, if any, applies the LACIS_* environment
// variables and decodes the result over DefaultConfig. It does not validate.
func Load(fs afero.Fs, file string) (*Config, error) {
	v := viper.New()
	v.SetFs(fs)
	v.SetConfigType("yaml")
	v.SetEnvPrefix(EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	cfg := DefaultConfig()
	for _, key := range Keys(reflect.ValueOf(*cfg), "") {
		if err := v.BindEnv(key); err != nil {
			return nil, err
		}
	}
	if file != "" {
		v.SetConfigFile(file)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("read config %s: %w", file, err)
		}
	}
	err := v.Unmarshal(cfg, viper.DecodeHook(mapstructure.ComposeDecodeHookFunc(
		DurationHookFunc(),
		ByteSizeHookFunc(),
		mapstructure.StringToSliceHookFunc(","),
	)))
	if err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	return cfg, nil
}

// Keys lists the configuration keys of v, following the mapstructure tags of
// nested structs and the entries of maps, so that each one can be overridden
// by the environment.
func Keys(v reflect.Value, prefix string) []string {
	var keys []string
	t := v.Type()
	for i := range t.NumField() {
		f := t.Field(i)
		tag := strings.Split(f.Tag.Get("mapstructure"), ",")[0]
		if tag == "" || tag == "-" {
			continue
		}
		key := prefix + tag
		switch {
		case f.Type.Kind() == reflect.Struct:
			keys = append(keys, Keys(v.Field(i), key+".")...)
		case f.Type.Kind() == reflect.Map && f.Type.Key().Kind() == reflect.String:
			for _, k := range v.Field(i).MapKeys() {
				keys = append(keys, key+"."+k.String())
			}
		default:
			keys = append(keys, key)
		}
	}
	return keys
}
