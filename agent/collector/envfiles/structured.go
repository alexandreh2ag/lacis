package envfiles

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/alexandreh2ag/lacis/agent/ini"
	"go.yaml.in/yaml/v3"
)

// A structured source (php, ini, yaml, json) is read into leaves keyed by
// their dotted path, such as doctrine.dbal.url. A declarative source
// flattens them into env keys: the path upper-cased, its dots replaced
// with "_" (DOCTRINE_DBAL_URL), so that allow_keys, deny_keys, the URL and
// DSN parsing and the X_HOST/X_PORT pairing apply as to a .env; the
// dependency keeps the dotted path as its key_path.

// maxLeaves bounds the values read in one file, against a YAML alias bomb.
const maxLeaves = 10000

var errTooManyLeaves = fmt.Errorf("more than %d values", maxLeaves)

// parseSource reads the leaves of a structured file.
func parseSource(format, file string, content []byte) (map[string]value, error) {
	switch format {
	case "php":
		return parsePHPConfig(file, string(content)), nil
	case "ini":
		return parseINIConfig(file, content)
	case "yaml":
		return parseYAMLConfig(file, content)
	case "json":
		return parseJSONConfig(file, content)
	}
	return nil, fmt.Errorf("%s: unknown format %q", file, format)
}

// envKey is the env key of a dotted path: DOCTRINE_DBAL_URL.
func envKey(keyPath string) string {
	return strings.ToUpper(strings.ReplaceAll(keyPath, ".", "_"))
}

// symfonyEnvRe matches a Symfony %env(processor:NAME)% reference.
var symfonyEnvRe = regexp.MustCompile(`%env\(([^()%]*)\)%`)

// symfonyProcessors keep the value as it is, for its host and port: the
// others (base64, json, file, key, url, query_string…) transform it.
var symfonyProcessors = map[string]bool{
	"string": true, "resolve": true, "int": true, "float": true, "bool": true, "not": true, "trim": true,
}

// literal returns the value of a literal string of a structured file, its
// Symfony %env()% references turned into ${NAME}, resolved by env.resolve.
// ok is false for a reference this reader cannot follow.
func literal(raw, file string, line int) (value, bool) {
	if !strings.Contains(raw, "%env(") {
		return scalarValue(raw, false, file, line), true
	}
	ok := true
	out := symfonyEnvRe.ReplaceAllStringFunc(raw, func(m string) string {
		name, good := symfonyEnvName(symfonyEnvRe.FindStringSubmatch(m)[1])
		if !good {
			ok = false
			return m
		}
		return "${" + name + "}"
	})
	// A "$" outside the references would be read as one.
	if !ok || strings.Contains(symfonyEnvRe.ReplaceAllString(raw, ""), "$") {
		return value{}, false
	}
	return scalarValue(out, true, file, line), true
}

// symfonyEnvName returns NAME of "proc:proc:NAME", the processors keeping
// the value; default:param:NAME skips its fallback parameter.
func symfonyEnvName(inner string) (string, bool) {
	parts := strings.Split(inner, ":")
	name := parts[len(parts)-1]
	procs := parts[:len(parts)-1]
	for i := 0; i < len(procs); i++ {
		switch {
		case procs[i] == "default":
			i++ // the fallback parameter
		case !symfonyProcessors[procs[i]]:
			return "", false
		}
	}
	return name, envName(name)
}

// leaves collects the values of a tree, keyed by path.
type leaves struct {
	file string
	out  map[string]value
}

func (l *leaves) add(keyPath, raw string, line int) error {
	if len(l.out) >= maxLeaves {
		return errTooManyLeaves
	}
	if v, ok := literal(raw, l.file, line); ok {
		l.out[keyPath] = v
	}
	return nil
}

func joinPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

// parseYAMLConfig reads the documents of a YAML file; a tagged value
// (!php/const, !tagged_iterator…) is not read.
func parseYAMLConfig(file string, content []byte) (map[string]value, error) {
	l := &leaves{file: file, out: map[string]value{}}
	dec := yaml.NewDecoder(bytes.NewReader(content))
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return l.out, nil
		}
		if err != nil {
			return l.out, fmt.Errorf("%s: %w", file, err)
		}
		if err := l.yaml("", &doc, 0); err != nil {
			return l.out, fmt.Errorf("%s: %w", file, err)
		}
	}
}

func (l *leaves) yaml(prefix string, n *yaml.Node, depth int) error {
	if depth > maxPHPDepth {
		return nil
	}
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			if err := l.yaml(prefix, c, depth+1); err != nil {
				return err
			}
		}
	case yaml.AliasNode:
		if n.Alias != nil {
			return l.yaml(prefix, n.Alias, depth+1)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			key := prefix
			if k.Value != "<<" { // a merge key adds its mapping here
				key = joinPath(prefix, k.Value)
			}
			if err := l.yaml(key, v, depth+1); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			if err := l.yaml(joinPath(prefix, strconv.Itoa(i)), c, depth+1); err != nil {
				return err
			}
		}
	case yaml.ScalarNode:
		if prefix != "" && (n.Tag == "" || strings.HasPrefix(n.Tag, "!!")) && n.Tag != "!!null" {
			return l.add(prefix, n.Value, n.Line)
		}
	}
	return nil
}

// parseJSONConfig reads a JSON file; its values have no line.
func parseJSONConfig(file string, content []byte) (map[string]value, error) {
	dec := json.NewDecoder(bytes.NewReader(content))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return map[string]value{}, fmt.Errorf("%s: %w", file, err)
	}
	l := &leaves{file: file, out: map[string]value{}}
	return l.out, l.json("", root)
}

func (l *leaves) json(prefix string, v any) error {
	switch v := v.(type) {
	case map[string]any:
		for k, c := range v {
			if err := l.json(joinPath(prefix, k), c); err != nil {
				return err
			}
		}
	case []any:
		for i, c := range v {
			if err := l.json(joinPath(prefix, strconv.Itoa(i)), c); err != nil {
				return err
			}
		}
	case string:
		return l.add(prefix, v, 0)
	case json.Number:
		return l.add(prefix, v.String(), 0)
	case bool:
		return l.add(prefix, strconv.FormatBool(v), 0)
	}
	return nil
}

// iniOptions read a quoted value whole, single or double quoted, and
// strip a ";" comment after it or after an unquoted value.
var iniOptions = ini.Options{Value: func(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw != "" && (raw[0] == '"' || raw[0] == '\'') {
		if end := strings.IndexByte(raw[1:], raw[0]); end >= 0 {
			return raw[1 : end+1]
		}
		return raw[1:]
	}
	if i := strings.Index(raw, ";"); i >= 0 {
		return strings.TrimSpace(raw[:i])
	}
	return raw
}}

// parseINIConfig reads an INI file: the path of a key is its section, then
// the key, its dots and PHP brackets (db[host]) splitting it further.
// Section inheritance ("[prod : base]") keeps the name alone.
func parseINIConfig(file string, content []byte) (map[string]value, error) {
	sections, err := ini.Parse(content, iniOptions)
	if err != nil {
		return map[string]value{}, fmt.Errorf("%s: %w", file, err)
	}
	l := &leaves{file: file, out: map[string]value{}}
	brackets := strings.NewReplacer("[", ".", "]", "")
	for _, s := range sections {
		section, _, _ := strings.Cut(s.Name, ":")
		section = strings.TrimSpace(section)
		for _, k := range s.Keys {
			key := strings.Trim(brackets.Replace(k.Name), ".")
			if key == "" {
				continue
			}
			if err := l.add(joinPath(section, key), k.Value, k.Line); err != nil {
				return l.out, err
			}
		}
	}
	return l.out, nil
}
