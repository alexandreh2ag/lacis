// Package ini is the one INI reader of the agent: supervisor, PHP-FPM and
// Zend_Config_Ini files. It splits a file into sections and keys; what a
// value means (quotes, inline comments) is left to the dialect, through
// Options.
package ini

import (
	"bufio"
	"bytes"
	"strings"
)

// MaxLine is the longest line read: a supervisor command or environment
// may be long.
const MaxLine = 1 << 20

// Section is a [name] of a file. The first section of Parse is the unnamed
// one, holding the keys before any header.
type Section struct {
	// Name is the text between the brackets, trimmed: "program:web",
	// "production : staging".
	Name string
	Line int
	Keys []Key
}

// Key is a "key = value" line.
type Key struct {
	Name  string
	Value string
	Line  int
}

// Options are the differences between the dialects.
type Options struct {
	// Continuation joins an indented line to the value of the previous
	// key, with a space: supervisor.
	Continuation bool
	// Value cleans a value, or one line of a continued value: inline
	// comment, quotes. The value is only trimmed when nil.
	Value func(raw string) string
}

// Parse reads the sections of an INI file. Blank lines and lines starting
// with ";" or "#" are skipped, as lines without "=".
func Parse(content []byte, opts Options) ([]Section, error) {
	clean := opts.Value
	if clean == nil {
		clean = strings.TrimSpace
	}
	sections := []Section{{}}
	current := &sections[0]
	var last *Key
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(nil, MaxLine)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		raw := scanner.Text()
		line := strings.TrimSpace(raw)
		switch {
		case line == "" || line[0] == ';' || line[0] == '#':
			continue
		case line[0] == '[':
			sections = append(sections, Section{Name: strings.TrimSpace(strings.Trim(line, "[]")), Line: lineNo})
			current, last = &sections[len(sections)-1], nil
			continue
		case opts.Continuation && (raw[0] == ' ' || raw[0] == '\t'):
			if last != nil {
				last.Value += " " + clean(line)
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		current.Keys = append(current.Keys, Key{Name: strings.TrimSpace(key), Value: clean(strings.TrimSpace(value)), Line: lineNo})
		last = &current.Keys[len(current.Keys)-1]
	}
	return sections, scanner.Err()
}

// Get returns the last value of key in s, as a later line overrides an
// earlier one.
func (s Section) Get(key string) (string, bool) {
	for i := len(s.Keys) - 1; i >= 0; i-- {
		if s.Keys[i].Name == key {
			return s.Keys[i].Value, true
		}
	}
	return "", false
}
