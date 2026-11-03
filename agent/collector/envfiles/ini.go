package envfiles

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/alexandreh2ag/lacis/agent/fsutil"
	"github.com/alexandreh2ag/lacis/agent/ini"
	"github.com/spf13/afero"
)

// Zend1Config is the configuration of a Zend Framework 1 application.
const Zend1Config = "application/configs/application.ini"

// block is a group of a structured configuration: the keys under one
// path, such as resources.db.params.{host,port,dbname}.
type block struct {
	path   string
	values map[string]value
}

// blockKeys make a block a dependency; dsn and url hold a whole URL.
var blockKeys = []string{"host", "hostname", "server", "dsn", "url"}

// adapterSchemes map the Zend_Db adapters to schemes.
var adapterSchemes = map[string]string{
	"pdo_mysql": "mysql", "mysqli": "mysql", "pdo_pgsql": "pgsql", "pdo_sqlsrv": "sqlsrv",
	"sqlsrv": "sqlsrv", "pdo_oci": "oracle", "oracle": "oracle", "pdo_sqlite": "sqlite",
	// Laminas\Db drivers, compared in lower case.
	"pgsql": "pgsql", "oci8": "oracle", "sqlite": "sqlite",
}

// iniSection is a section of an INI file and the one it extends.
type iniSection struct {
	parent string
	keys   []string
	values map[string]value
}

// zend1Blocks reads application.ini, in the section of the env of the app
// merged with the sections it extends, and returns its blocks.
func (r *reader) zend1Blocks(root, env string) ([]block, error) {
	file := path.Join(root, Zend1Config)
	sections, order, err := readINI(r.fs, file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	name := zend1Env(r.fs, root)
	for _, candidate := range []string{name, env, "production"} {
		if _, ok := sections[candidate]; ok && candidate != "" {
			name = candidate
			break
		}
	}
	if _, ok := sections[name]; !ok && len(order) > 0 {
		name = order[0]
	}
	merged := map[string]value{}
	for _, s := range chain(sections, name) {
		for _, k := range s.keys {
			merged[k] = s.values[k]
		}
	}
	return blocks(merged), nil
}

// htaccessEnv reads SetEnv APPLICATION_ENV in the .htaccess of public/.
var htaccessEnv = regexp.MustCompile(`(?mi)^\s*SetEnv\s+APPLICATION_ENV\s+"?([\w-]+)"?`)

func zend1Env(fsys afero.Fs, root string) string {
	b, err := fsutil.ReadAppFile(fsys, path.Join(root, "public", ".htaccess"))
	if err != nil {
		return ""
	}
	if m := htaccessEnv.FindSubmatch(b); m != nil {
		return string(m[1])
	}
	return ""
}

// chain returns the sections a section extends, the root first.
func chain(sections map[string]*iniSection, name string) []*iniSection {
	var out []*iniSection
	for seen := map[string]bool{}; name != "" && !seen[name]; {
		s, ok := sections[name]
		if !ok {
			break
		}
		seen[name] = true
		out = append([]*iniSection{s}, out...)
		name = s.parent
	}
	return out
}

// zend1Options are those of Zend_Config_Ini: a quoted value is kept
// whole, ";" starts a comment after an unquoted one.
var zend1Options = ini.Options{Value: func(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, `"`) {
		return strings.Trim(raw, `"`)
	}
	if i := strings.Index(raw, ";"); i >= 0 {
		return strings.TrimSpace(raw[:i])
	}
	return raw
}}

// readINI reads the sections of an INI file in the Zend_Config_Ini format:
// [name : parent], key = value, values quoted or not, ";" comments.
func readINI(fsys afero.Fs, file string) (map[string]*iniSection, []string, error) {
	b, err := fsutil.ReadAppFile(fsys, file)
	if err != nil {
		return nil, nil, err
	}
	parsed, err := ini.Parse(b, zend1Options)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", file, err)
	}
	sections := map[string]*iniSection{}
	var order []string
	for _, ps := range parsed[1:] { // the keys before any section are ignored
		name, parent, _ := strings.Cut(ps.Name, ":")
		name = strings.TrimSpace(name)
		current := &iniSection{parent: strings.TrimSpace(parent), values: map[string]value{}}
		for _, k := range ps.Keys {
			if _, seen := current.values[k.Name]; !seen {
				current.keys = append(current.keys, k.Name)
			}
			current.values[k.Name] = value{value: k.Value, file: file, line: k.Line}
		}
		sections[name] = current
		order = append(order, name)
	}
	return sections, order, nil
}

// blocks groups dotted keys by their parent path, and keeps the groups
// holding a key of blockKeys; their adapter, one level up, is kept too.
func blocks(values map[string]value) []block {
	groups := map[string]map[string]value{}
	for key, v := range values {
		i := strings.LastIndex(key, ".")
		if i < 0 {
			continue
		}
		parent, leaf := key[:i], strings.ToLower(key[i+1:])
		if groups[parent] == nil {
			groups[parent] = map[string]value{}
		}
		groups[parent][leaf] = v
	}
	var out []block
	for _, p := range sortedKeys(groups) {
		g := groups[p]
		if !slices.ContainsFunc(blockKeys, func(k string) bool { _, ok := g[k]; return ok }) {
			continue
		}
		if i := strings.LastIndex(p, "."); i >= 0 {
			if adapter, ok := values[p[:i]+".adapter"]; ok {
				g["adapter"] = adapter
			}
		}
		out = append(out, block{path: p, values: g})
	}
	return out
}
