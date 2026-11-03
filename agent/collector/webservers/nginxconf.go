package webservers

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/alexandreh2ag/lacis/agent/fsutil"
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

// nginxReader reads an nginx configuration from its main file, includes
// expanded: nginx is never run, its files are read with the rights of its
// master process.
type nginxReader struct {
	// read reads a file, glob lists the files of an include.
	read func(file string) ([]byte, error)
	glob func(pattern string) ([]string, error)
	// confDir is the directory of the main file: relative includes are
	// relative to it.
	confDir string
	// errs are the included files that cannot be read: reported, the rest
	// of the configuration still read.
	errs []error
}

// file parses a configuration file, its includes expanded.
func (r *nginxReader) file(name string, depth int) ([]directive, error) {
	b, err := r.read(name)
	if err != nil {
		return nil, err
	}
	directives, err := parse(name, string(b))
	if err != nil {
		return nil, err
	}
	return r.expand(directives, depth)
}

// expand replaces the include directives by the directives of the files
// they match, in the order of their names as nginx does.
func (r *nginxReader) expand(directives []directive, depth int) ([]directive, error) {
	var out []directive
	for _, d := range directives {
		if d.Name == "include" && len(d.Args) == 1 && d.Block == nil {
			if depth >= maxIncludeDepth {
				return nil, fmt.Errorf("%s: includes nested too deep", d.File)
			}
			pattern := d.Args[0]
			if !path.IsAbs(pattern) {
				pattern = path.Join(r.confDir, pattern)
			}
			files, _ := r.glob(pattern) // only ErrBadPattern
			slices.Sort(files)
			if len(files) == 0 && !strings.ContainsAny(pattern, "*?[") {
				r.errs = append(r.errs, fmt.Errorf("%s: include %s not found", d.File, pattern))
			}
			for _, name := range files {
				included, err := r.file(name, depth+1)
				if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) || errors.Is(err, fsutil.ErrNotRegular) || errors.Is(err, fsutil.ErrTooLarge) {
					r.errs = append(r.errs, err)
					continue
				}
				if err != nil {
					return nil, err
				}
				out = append(out, included...)
			}
			continue
		}
		if d.Block != nil {
			block, err := r.expand(d.Block, depth)
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
