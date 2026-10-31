package config

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/afero"
	"github.com/spf13/viper"
)

// Extensions are the accepted configuration file extensions, by priority.
var Extensions = []string{".yaml", ".yml"}

// FindFile returns the configuration file designated by path: path itself
// when it is a file, otherwise <path>/<name>.yaml or <path>/<name>.yml. When
// both exist, the .yaml file wins and the other one is returned in ignored.
// file is empty when no file exists, which is valid: the configuration can
// come from the environment only.
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
	for _, key := range Keys(reflect.TypeOf(Config{}), "") {
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
	cfg := DefaultConfig()
	err := v.Unmarshal(cfg, viper.DecodeHook(mapstructure.ComposeDecodeHookFunc(
		mapstructure.StringToTimeDurationHookFunc(),
		mapstructure.StringToSliceHookFunc(","),
	)))
	if err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	return cfg, nil
}

var durationType = reflect.TypeOf(time.Duration(0))

// Keys lists the configuration keys of t, following the mapstructure tags of
// nested structs, so that each one can be overridden by the environment.
func Keys(t reflect.Type, prefix string) []string {
	var keys []string
	for i := range t.NumField() {
		f := t.Field(i)
		tag := strings.Split(f.Tag.Get("mapstructure"), ",")[0]
		if tag == "" || tag == "-" {
			continue
		}
		key := prefix + tag
		if f.Type.Kind() == reflect.Struct && f.Type != durationType {
			keys = append(keys, Keys(f.Type, key+".")...)
			continue
		}
		keys = append(keys, key)
	}
	return keys
}
