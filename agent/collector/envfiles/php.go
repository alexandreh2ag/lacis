package envfiles

import (
	"sort"
	"strconv"
	"strings"
)

// The PHP configuration of an app (Laminas config/autoload/*.php, Laravel
// config/*.php, a declarative php source) is read by a static parser, never
// executed: the file must return a literal array. Its values may be
// literals (strings, numbers, booleans, null), nested arrays in the array()
// or [] syntax, concatenations, and the environment through getenv('X'),
// $_ENV['X'], $_SERVER['X'] or env('X', default), with ?: and ?? giving a
// default. Such a value becomes a reference, ${X} or ${X:-default}, that
// env.resolve resolves against the env of the app. Any other expression (a
// constant, a function call, a variable, a closure) is an unparsed value:
// its key is skipped.

// maxPHPDepth bounds the nesting of the arrays read.
const maxPHPDepth = 64

// phpExpr is a parsed expression: a scalar (raw, a reference when interp),
// an array, or an unparsed value.
type phpExpr struct {
	kind    phpKind
	raw     string
	interp  bool
	line    int
	entries []phpEntry
}

type phpKind int

const (
	phpUnparsed phpKind = iota
	phpScalar
	phpArray
)

type phpEntry struct {
	key   string
	value phpExpr
}

type phpParser struct {
	s     string
	i     int
	lines []int // offsets of the line starts
	depth int
	// inValue is true while reading the value of an entry: an unparsed
	// value may hold a "=>" (fn() => 1), an unparsed key ends at it.
	inValue bool
}

// parsePHPConfig returns the leaves of the array a PHP file returns, keyed
// by their dotted path; a file returning anything else gives none.
func parsePHPConfig(file, content string) map[string]value {
	p := &phpParser{s: content, lines: []int{0}}
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			p.lines = append(p.lines, i+1)
		}
	}
	out := map[string]value{}
	if !p.seekReturn() {
		return out
	}
	expr := p.expr()
	if expr.kind == phpArray {
		flattenPHP(out, "", expr, file)
	}
	return out
}

func flattenPHP(out map[string]value, prefix string, e phpExpr, file string) {
	for _, entry := range e.entries {
		key := entry.key
		if prefix != "" {
			key = prefix + "." + key
		}
		switch entry.value.kind {
		case phpArray:
			flattenPHP(out, key, entry.value, file)
		case phpScalar:
			if entry.value.interp {
				out[key] = scalarValue(entry.value.raw, true, file, entry.value.line)
			} else if v, ok := literal(entry.value.raw, file, entry.value.line); ok {
				out[key] = v
			}
		}
	}
}

// scalarValue is a value read in a structured configuration: a reference
// is resolved later by env.resolve.
func scalarValue(raw string, interp bool, file string, line int) value {
	if interp {
		return value{value: raw, raw: raw, interp: true, file: file, line: line}
	}
	return value{value: raw, file: file, line: line}
}

func (p *phpParser) line(at int) int {
	return sort.Search(len(p.lines), func(i int) bool { return p.lines[i] > at })
}

// seekReturn moves past the first return statement outside any block,
// string or comment.
func (p *phpParser) seekReturn() bool {
	braces := 0
	for p.i < len(p.s) {
		if p.skipTrivia() || p.skipString() {
			continue
		}
		c := p.s[p.i]
		switch {
		case c == '{':
			braces++
		case c == '}':
			braces--
		case isIdentStart(c):
			afterVar := p.i > 0 && (p.s[p.i-1] == '$' || p.s[p.i-1] == '>' || p.s[p.i-1] == ':')
			word := p.ident()
			if braces == 0 && !afterVar && strings.EqualFold(word, "return") {
				return true
			}
			continue
		}
		p.i++
	}
	return false
}

// skipTrivia skips white space and comments, the <?php tag included;
// false when there was none.
func (p *phpParser) skipTrivia() bool {
	start := p.i
	for p.i < len(p.s) {
		switch rest := p.s[p.i:]; {
		case rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n' || rest[0] == '\r':
			p.i++
		case strings.HasPrefix(rest, "<?php"):
			p.i += 5
		case strings.HasPrefix(rest, "//") || rest[0] == '#' && !strings.HasPrefix(rest, "#["):
			end := strings.IndexByte(rest, '\n')
			if end < 0 {
				end = len(rest)
			}
			p.i += end
		case strings.HasPrefix(rest, "/*"):
			end := strings.Index(rest[2:], "*/")
			if end < 0 {
				p.i = len(p.s)
			} else {
				p.i += end + 4
			}
		default:
			return p.i > start
		}
	}
	return p.i > start
}

// skipString skips a string literal or a heredoc; false when there is none.
func (p *phpParser) skipString() bool {
	if p.i >= len(p.s) {
		return false
	}
	switch c := p.s[p.i]; {
	case c == '\'' || c == '"':
		_, _, _ = p.quoted()
		return true
	case strings.HasPrefix(p.s[p.i:], "<<<"):
		p.heredoc()
		return true
	}
	return false
}

// quoted reads a quoted string at p.i: its value, and whether it has an
// interpolation ($var, {$expr}) that makes it unparsed.
func (p *phpParser) quoted() (string, bool, bool) {
	q := p.s[p.i]
	p.i++
	var b strings.Builder
	interpolated := false
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c == q:
			p.i++
			return b.String(), interpolated, true
		case c == '\\' && p.i+1 < len(p.s):
			next := p.s[p.i+1]
			p.i += 2
			if q == '\'' {
				if next != '\'' && next != '\\' {
					b.WriteByte('\\')
				}
				b.WriteByte(next)
				continue
			}
			switch next {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '"', '\\', '$':
				b.WriteByte(next)
			default:
				b.WriteByte('\\')
				b.WriteByte(next)
			}
			continue
		case q == '"' && c == '$' && p.i+1 < len(p.s) && (isIdentStart(p.s[p.i+1]) || p.s[p.i+1] == '{'),
			q == '"' && c == '{' && p.i+1 < len(p.s) && p.s[p.i+1] == '$':
			interpolated = true
		}
		b.WriteByte(c)
		p.i++
	}
	return "", false, false
}

// heredoc skips a heredoc or a nowdoc: <<<ID ... ID.
func (p *phpParser) heredoc() {
	p.i += 3
	rest := p.s[p.i:]
	nl := strings.IndexByte(rest, '\n')
	if nl < 0 {
		p.i = len(p.s)
		return
	}
	id := strings.Trim(strings.TrimSpace(rest[:nl]), `'"`)
	p.i += nl + 1
	for p.i < len(p.s) {
		end := strings.IndexByte(p.s[p.i:], '\n')
		lineEnd := len(p.s)
		if end >= 0 {
			lineEnd = p.i + end
		}
		line := strings.TrimLeft(p.s[p.i:lineEnd], " \t")
		if id != "" && strings.HasPrefix(line, id) && (len(line) == len(id) || !isIdentChar(line[len(id)])) {
			p.i = lineEnd - len(line) + len(id)
			return
		}
		p.i = lineEnd + 1
	}
	p.i = len(p.s)
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '\\' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isIdentChar(c byte) bool { return isIdentStart(c) || c >= '0' && c <= '9' }

// ident reads a name, namespace separators included.
func (p *phpParser) ident() string {
	start := p.i
	for p.i < len(p.s) && isIdentChar(p.s[p.i]) {
		p.i++
	}
	return p.s[start:p.i]
}

func (p *phpParser) peek(tok string) bool {
	p.skipTrivia()
	return strings.HasPrefix(p.s[p.i:], tok)
}

func (p *phpParser) accept(tok string) bool {
	if p.peek(tok) {
		p.i += len(tok)
		return true
	}
	return false
}

// atEnd tells the end of an expression: a separator of an array, of a
// call or of a statement.
func (p *phpParser) atEnd() bool {
	p.skipTrivia()
	if p.i >= len(p.s) {
		return true
	}
	switch p.s[p.i] {
	case ',', ']', ')', ';':
		return true
	}
	return p.peek("=>")
}

// expr reads an expression: operands joined by ".", "??" or "?:".
func (p *phpParser) expr() phpExpr {
	left := p.unary()
	for {
		switch {
		case p.atEnd():
			return left
		case p.accept("??"), p.accept("?:"):
			left = orDefault(left, p.unary())
		case p.peek(".") && !p.peek(".."):
			p.i++
			left = concat(left, p.unary())
		default:
			// An operator or a call this parser does not read.
			p.skipExpr()
			return phpExpr{}
		}
	}
}

// orDefault is left ?? right: a reference with a literal default, or a
// literal left.
func orDefault(left, right phpExpr) phpExpr {
	if left.kind != phpScalar || right.kind != phpScalar {
		return phpExpr{line: left.line}
	}
	if !left.interp {
		return left
	}
	name, ok := singleRef(left.raw)
	if !ok || right.interp || strings.ContainsAny(right.raw, "$}") {
		return phpExpr{line: left.line}
	}
	return phpExpr{kind: phpScalar, raw: "${" + name + ":-" + right.raw + "}", interp: true, line: left.line}
}

// singleRef returns X when raw is exactly ${X}.
func singleRef(raw string) (string, bool) {
	name, ok := strings.CutPrefix(raw, "${")
	if !ok {
		return "", false
	}
	name, ok = strings.CutSuffix(name, "}")
	return name, ok && envName(name)
}

func concat(left, right phpExpr) phpExpr {
	if left.kind != phpScalar || right.kind != phpScalar {
		return phpExpr{line: left.line}
	}
	return joinScalars(left.line, left, right)
}

// joinScalars concatenates scalars; a literal holding a "$" next to a
// reference would be read as one, so it makes the value unparsed.
func joinScalars(line int, parts ...phpExpr) phpExpr {
	out := phpExpr{kind: phpScalar, line: line}
	for _, part := range parts {
		out.interp = out.interp || part.interp
	}
	for _, part := range parts {
		if out.interp && !part.interp && strings.Contains(part.raw, "$") {
			return phpExpr{line: line}
		}
		out.raw += part.raw
	}
	return out
}

var phpCasts = map[string]bool{"int": true, "integer": true, "string": true, "bool": true, "boolean": true, "float": true, "double": true}

// unary reads a cast (ignored) and an operand.
func (p *phpParser) unary() phpExpr {
	p.skipTrivia()
	if p.peek("(") {
		save := p.i
		p.i++
		p.skipTrivia()
		name := strings.ToLower(p.ident())
		if phpCasts[name] && p.accept(")") {
			return p.unary()
		}
		p.i = save
	}
	return p.primary()
}

func (p *phpParser) primary() phpExpr {
	p.skipTrivia()
	line := p.line(p.i)
	if p.i >= len(p.s) {
		return phpExpr{line: line}
	}
	c := p.s[p.i]
	switch {
	case c == '[':
		p.i++
		return p.array("]", line)
	case c == '(':
		p.i++
		e := p.expr()
		if !p.accept(")") {
			p.skipTo(")")
			return phpExpr{line: line}
		}
		return e
	case c == '\'' || c == '"':
		s, interpolated, ok := p.quoted()
		if !ok || interpolated {
			return phpExpr{line: line}
		}
		return phpExpr{kind: phpScalar, raw: s, line: line}
	case c == '-' || c >= '0' && c <= '9':
		start := p.i
		p.i++
		for p.i < len(p.s) && (isIdentChar(p.s[p.i]) || p.s[p.i] == '.' && p.i+1 < len(p.s) && p.s[p.i+1] >= '0' && p.s[p.i+1] <= '9') {
			p.i++
		}
		raw := strings.ReplaceAll(p.s[start:p.i], "_", "")
		if _, err := strconv.ParseFloat(raw, 64); err != nil {
			if _, err := strconv.ParseInt(raw, 0, 64); err != nil {
				return phpExpr{line: line}
			}
		}
		return phpExpr{kind: phpScalar, raw: raw, line: line}
	case c == '$':
		p.i++
		name := p.ident()
		if (name == "_ENV" || name == "_SERVER") && p.accept("[") {
			if arg, ok := p.stringArg(); ok && p.accept("]") {
				return envRef(arg, "", line)
			}
			p.skipTo("]")
		}
		p.skipExpr()
		return phpExpr{line: line}
	case isIdentStart(c):
		name := strings.TrimPrefix(p.ident(), "\\")
		switch lower := strings.ToLower(name); {
		case lower == "true" || lower == "false":
			return phpExpr{kind: phpScalar, raw: lower, line: line}
		case lower == "null":
			return phpExpr{kind: phpScalar, line: line}
		case lower == "array" && p.accept("("):
			return p.array(")", line)
		case (lower == "getenv" || lower == "env") && p.accept("("):
			return p.envCall(lower, line)
		}
	}
	p.skipExpr()
	return phpExpr{line: line}
}

// envCall reads getenv('X') or env('X', default) after its "(".
func (p *phpParser) envCall(fn string, line int) phpExpr {
	name, ok := p.stringArg()
	if !ok {
		p.skipTo(")")
		return phpExpr{line: line}
	}
	def := ""
	if fn == "env" && p.accept(",") {
		d := p.expr()
		if d.kind != phpScalar || d.interp {
			p.skipTo(")")
			return phpExpr{line: line}
		}
		def = d.raw
		if def == "" {
			def = "\x00" // a default, empty: still ${X:-}
		}
	}
	if !p.accept(")") {
		p.skipTo(")")
		return phpExpr{line: line}
	}
	return envRef(name, def, line)
}

// envRef is a reference to the variable name, with a default when def is
// set ("\x00" for an empty one).
func envRef(name, def string, line int) phpExpr {
	if !envName(name) || strings.ContainsAny(def, "$}") {
		return phpExpr{line: line}
	}
	if def == "" {
		return phpExpr{kind: phpScalar, raw: "${" + name + "}", interp: true, line: line}
	}
	return phpExpr{kind: phpScalar, raw: "${" + name + ":-" + strings.TrimPrefix(def, "\x00") + "}", interp: true, line: line}
}

// envName tells a name a reference can hold.
func envName(name string) bool {
	if name == "" || name[0] >= '0' && name[0] <= '9' {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c != '_' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// stringArg reads a literal string argument.
func (p *phpParser) stringArg() (string, bool) {
	p.skipTrivia()
	if p.i >= len(p.s) || p.s[p.i] != '\'' && p.s[p.i] != '"' {
		return "", false
	}
	s, interpolated, ok := p.quoted()
	return s, ok && !interpolated
}

// array reads the entries of an array up to its closing token. A key that
// is not a literal skips its entry; integer keys continue from the
// greatest one, like PHP.
func (p *phpParser) array(closing string, line int) phpExpr {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxPHPDepth {
		p.skipTo(closing)
		return phpExpr{line: line}
	}
	out := phpExpr{kind: phpArray, line: line}
	next := 0
	for {
		p.skipTrivia()
		if p.i >= len(p.s) {
			return phpExpr{line: line}
		}
		if p.accept(closing) {
			return out
		}
		saved := p.inValue
		p.inValue = false
		first := p.expr()
		key, keyOK := "", true
		val := first
		if p.accept("=>") {
			key, keyOK = first.raw, first.kind == phpScalar && !first.interp
			p.inValue = true
			val = p.expr()
			if n, err := strconv.Atoi(key); err == nil && keyOK && n >= next {
				next = n + 1
			}
		} else {
			key = strconv.Itoa(next)
			next++
		}
		p.inValue = saved
		if keyOK && key != "" {
			out.entries = append(out.entries, phpEntry{key: key, value: val})
		}
		p.skipTrivia()
		if !p.accept(",") && !p.peek(closing) {
			// Not a well-formed array: give up on it.
			p.skipTo(closing)
			return phpExpr{line: line}
		}
	}
}

// skipExpr skips the rest of an expression: up to a separator outside any
// bracket, string or comment.
func (p *phpParser) skipExpr() {
	depth := 0
	for p.i < len(p.s) {
		if p.skipTrivia() || p.skipString() {
			continue
		}
		switch p.s[p.i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				return
			}
			depth--
		case ',', ';':
			if depth == 0 {
				return
			}
		case '=':
			if depth == 0 && !p.inValue && strings.HasPrefix(p.s[p.i:], "=>") {
				return
			}
		}
		p.i++
	}
}

// skipTo skips past the closing token of the current bracket.
func (p *phpParser) skipTo(closing string) {
	for p.i < len(p.s) {
		p.skipExpr()
		if p.accept(closing) {
			return
		}
		if p.i < len(p.s) {
			p.i++
		}
	}
}
