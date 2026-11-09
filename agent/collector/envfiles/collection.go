package envfiles

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/alexandreh2ag/lacis/agent/config"
	"github.com/alexandreh2ag/lacis/common/dsn"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"go.yaml.in/yaml/v3"
)

// A value may hold a JSON or YAML collection, such as
// ELASTIC_SERVERS='[{"host":"10.0.0.1","port":9200}]' or
// ELASTICSEARCH_SERVERS='["10.0.0.1:9200","10.0.0.2:9200"]': each item is a
// dependency of the key. A mapping is read as a block (host, port, dsn…,
// the denied names never), a scalar as a URL, a DSN or a host:port. YAML
// reads JSON as well.

// collectionItem is an item of a collection: a scalar, or the leaves of a
// mapping.
type collectionItem struct {
	index  string // "" for a mapping at the root
	scalar *value
	values map[string]value
}

// looksLikeCollection tells a value worth decoding: almost any string is
// a YAML scalar, so only a flow collection, a block sequence or a value on
// several lines is.
func looksLikeCollection(raw string) bool {
	s := strings.TrimSpace(raw)
	return strings.HasPrefix(s, "[") || strings.HasPrefix(s, "{") || strings.HasPrefix(s, "- ") || strings.Contains(s, "\n")
}

// parseCollection decodes a collection: ok is false when the value is not
// one (a scalar, an invalid document), which is then parsed as a value.
// The items take the file and line of v.
func parseCollection(v value) ([]collectionItem, bool) {
	if !looksLikeCollection(v.value) {
		return nil, false
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(v.value), &doc); err != nil || len(doc.Content) != 1 {
		return nil, false
	}
	root := doc.Content[0]
	for root.Kind == yaml.AliasNode && root.Alias != nil {
		root = root.Alias
	}
	if root.Kind != yaml.SequenceNode && root.Kind != yaml.MappingNode {
		return nil, false
	}
	// One walk over the whole document, bounded by maxLeaves.
	l := &leaves{file: v.file, out: map[string]value{}}
	_ = l.yaml("", &doc, 0) // past maxLeaves, the leaves read are kept
	for k, leaf := range l.out {
		leaf.file, leaf.line = v.file, v.line
		l.out[k] = leaf
	}
	if root.Kind == yaml.MappingNode {
		return []collectionItem{{values: l.out}}, true
	}
	byIndex := map[int]*collectionItem{}
	for path, leaf := range l.out {
		head, rest, nested := strings.Cut(path, ".")
		i, err := strconv.Atoi(head)
		if err != nil {
			continue
		}
		item, ok := byIndex[i]
		if !ok {
			item = &collectionItem{index: head}
			byIndex[i] = item
		}
		if !nested {
			item.scalar = &leaf
			continue
		}
		if item.values == nil {
			item.values = map[string]value{}
		}
		item.values[rest] = leaf
	}
	indexes := make([]int, 0, len(byIndex))
	for i := range byIndex {
		indexes = append(indexes, i)
	}
	slices.Sort(indexes)
	items := make([]collectionItem, 0, len(indexes))
	for _, i := range indexes {
		items = append(items, *byIndex[i])
	}
	return items, true
}

// collectionDependencies turns the items of the collection held by key
// into dependencies of key, each keeping its path as key_path. A
// collection giving nothing is unparsed.
func collectionDependencies(key string, v value, items []collectionItem, e env, cfg config.EnvfilesConfig, res *resolver) []snapshot.Dependency {
	base := v.keyPath
	if base == "" {
		base = key
	}
	out := []snapshot.Dependency{}
	skipped := false
	for _, item := range items {
		keyPath := base
		if item.index != "" {
			keyPath += "." + item.index
		}
		source := snapshot.DependencySource{File: v.file, Line: v.line, KeyPath: keyPath}
		if item.scalar == nil {
			e.resolveValues(item.values)
			deps := blockDependencies([]block{{path: keyPath, values: item.values}}, cfg, res)
			for i := range deps {
				deps[i].Key = key
			}
			out = append(out, deps...)
			continue
		}
		scalar := map[string]value{"": *item.scalar}
		e.resolveValues(scalar)
		raw := scalar[""].value
		if scalar[""].unresolved || !networkShaped(raw) {
			out = append(out, snapshot.Dependency{Key: key, Source: source, Unparsed: true})
			continue
		}
		all, err := dsn.ParseAll(raw)
		if errors.Is(err, dsn.ErrNotNetwork) {
			skipped = true
			continue
		}
		if err != nil {
			out = append(out, snapshot.Dependency{Key: key, Source: source, Unparsed: true})
			continue
		}
		for _, d := range all {
			out = append(out, dependency(key, source, d, res))
		}
	}
	if len(out) == 0 && !skipped {
		return []snapshot.Dependency{{Key: key, Source: snapshot.DependencySource{File: v.file, Line: v.line, KeyPath: v.keyPath}, Unparsed: true}}
	}
	return out
}

// networkShaped tells a scalar of a collection unambiguously a target: a
// URL, a DSN, a socket, an IP, or a name with a port or a dot. A bare word
// may be anything, a secret among others: it is neither sent nor resolved.
func networkShaped(raw string) bool {
	s := strings.TrimSpace(raw)
	if strings.Contains(s, "://") || strings.Contains(s, "=") || strings.HasPrefix(s, "/") {
		return true
	}
	host, port, err := dsn.ParseHostPort(s)
	if err != nil {
		return false
	}
	return port != 0 || dsn.HostKind(host) == dsn.KindIP || strings.Contains(host, ".")
}
