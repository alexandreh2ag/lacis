package webservers

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
)

// directive is an nginx directive: "name args;" or "name args { block }".
type directive struct {
	Name  string
	Args  []string
	Block []directive
	// File is the configuration file declaring the directive.
	File string
}

// maxIncludeDepth bounds the nested includes, against include loops.
const maxIncludeDepth = 8

const dumpFileMarker = "# configuration file "

// splitDump splits the output of nginx -T into its files, in order: each one
// starts with "# configuration file <path>:".
func splitDump(r io.Reader) (files map[string]string, order []string, err error) {
	files = map[string]string{}
	var current string
	var b strings.Builder
	flush := func() {
		if current != "" {
			files[current] = b.String()
		}
		b.Reset()
	}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if name, ok := strings.CutPrefix(line, dumpFileMarker); ok && strings.HasSuffix(name, ":") {
			flush()
			current = strings.TrimSuffix(name, ":")
			order = append(order, current)
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	flush()
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	if len(order) == 0 {
		return nil, nil, errors.New("no configuration file in nginx -T output")
	}
	return files, order, nil
}

// parseDump parses the configuration of nginx -T from its main file,
// includes expanded.
func parseDump(r io.Reader) ([]directive, error) {
	files, order, err := splitDump(r)
	if err != nil {
		return nil, err
	}
	main := order[0]
	p := &dumpParser{files: files, names: order, confDir: path.Dir(main)}
	return p.file(main, 0)
}

type dumpParser struct {
	files   map[string]string
	names   []string
	confDir string
}

func (p *dumpParser) file(name string, depth int) ([]directive, error) {
	directives, err := parse(name, p.files[name])
	if err != nil {
		return nil, err
	}
	return p.expand(directives, depth)
}

// expand replaces the include directives by the directives of the files
// they match, relative paths being relative to the main file directory.
func (p *dumpParser) expand(directives []directive, depth int) ([]directive, error) {
	var out []directive
	for _, d := range directives {
		if d.Name == "include" && len(d.Args) == 1 && d.Block == nil {
			if depth >= maxIncludeDepth {
				return nil, fmt.Errorf("%s: includes nested too deep", d.File)
			}
			pattern := d.Args[0]
			if !path.IsAbs(pattern) {
				pattern = path.Join(p.confDir, pattern)
			}
			for _, name := range p.names {
				if ok, _ := path.Match(pattern, name); ok {
					included, err := p.file(name, depth+1)
					if err != nil {
						return nil, err
					}
					out = append(out, included...)
				}
			}
			continue
		}
		if d.Block != nil {
			block, err := p.expand(d.Block, depth)
			if err != nil {
				return nil, err
			}
			d.Block = block
		}
		out = append(out, d)
	}
	return out, nil
}

// parse reads the directives of one nginx configuration file.
func parse(file, content string) ([]directive, error) {
	tokens, err := tokenize(content)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	directives, rest, err := parseBlock(file, tokens, false)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	if len(rest) > 0 {
		return nil, fmt.Errorf("%s: unexpected %q", file, rest[0].text)
	}
	return directives, nil
}

type token struct {
	text string
	// special is true for an unquoted ";", "{" or "}".
	special bool
}

func parseBlock(file string, tokens []token, nested bool) ([]directive, []token, error) {
	directives := []directive{}
	for len(tokens) > 0 {
		t := tokens[0]
		if t.special && t.text == "}" {
			if !nested {
				return nil, nil, errors.New(`unexpected "}"`)
			}
			return directives, tokens[1:], nil
		}
		if t.special {
			return nil, nil, fmt.Errorf("unexpected %q", t.text)
		}
		d := directive{Name: t.text, File: file}
		tokens = tokens[1:]
		for {
			if len(tokens) == 0 {
				return nil, nil, fmt.Errorf("directive %q not terminated", d.Name)
			}
			t = tokens[0]
			tokens = tokens[1:]
			if !t.special {
				d.Args = append(d.Args, t.text)
				continue
			}
			switch t.text {
			case "{":
				block, rest, err := parseBlock(file, tokens, true)
				if err != nil {
					return nil, nil, err
				}
				d.Block, tokens = block, rest
			case "}":
				return nil, nil, fmt.Errorf("directive %q not terminated", d.Name)
			}
			break
		}
		directives = append(directives, d)
	}
	if nested {
		return nil, nil, errors.New(`missing "}"`)
	}
	return directives, nil, nil
}

// tokenize splits an nginx configuration into words, quoted strings and the
// special characters ";", "{" and "}". Comments are dropped.
func tokenize(s string) ([]token, error) {
	var tokens []token
	for i := 0; i < len(s); {
		c := s[i]
		switch c {
		case ' ', '\t', '\n', '\r':
			i++
		case '#':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case ';', '{', '}':
			tokens = append(tokens, token{text: string(c), special: true})
			i++
		case '"', '\'':
			var b strings.Builder
			j := i + 1
			for ; j < len(s) && s[j] != c; j++ {
				if s[j] == '\\' && j+1 < len(s) {
					j++
				}
				b.WriteByte(s[j])
			}
			if j >= len(s) {
				return nil, errors.New("unterminated quoted string")
			}
			tokens = append(tokens, token{text: b.String()})
			i = j + 1
		default:
			j := i
			// "{" belongs to a word such as ${var} or a regex quantifier.
			depth := 0
			for ; j < len(s); j++ {
				ch := s[j]
				if ch == '{' && j > i && s[j-1] == '$' {
					depth++
					continue
				}
				if ch == '}' && depth > 0 {
					depth--
					continue
				}
				if slices.Contains([]byte(" \t\n\r;{}"), ch) {
					break
				}
				if ch == '\\' && j+1 < len(s) {
					j++
				}
			}
			tokens = append(tokens, token{text: s[i:j]})
			i = j
		}
	}
	return tokens, nil
}
