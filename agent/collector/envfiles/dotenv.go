package envfiles

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// value is a configuration value and where it was read.
type value struct {
	value string
	file  string
	line  int
	// raw holds the references of an interpolated value, resolved by
	// env.resolve once every source is read.
	raw    string
	interp bool
	// unresolved is true when the value refers to a variable defined
	// nowhere: it cannot be trusted.
	unresolved bool
}

// env is the configuration of an app, merged in override order.
type env map[string]value

// set records an interpolated value, resolved later.
func (e env) set(key, raw, file string, line int) {
	e[key] = value{value: raw, raw: raw, interp: true, file: file, line: line}
}

// reference matches ${VAR}, ${VAR:-default} and $VAR.
var reference = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// maxDepth bounds the references followed, against loops.
const maxDepth = 16

// resolve resolves the references of the interpolated values against the
// final values, whatever the file defining them: a value of .env may refer
// to a variable that .env.local defines.
func (e env) resolve() {
	done := map[string]bool{}
	var get func(name string, depth int) (string, bool)
	get = func(name string, depth int) (string, bool) {
		v, ok := e[name]
		switch {
		case !ok:
			return "", false
		case !v.interp || done[name]:
			return v.value, !v.unresolved
		case depth > maxDepth:
			return "", false
		}
		v.value, ok = expand(v.raw, func(ref string) (string, bool) { return get(ref, depth+1) })
		v.unresolved = !ok
		e[name], done[name] = v, true
		return v.value, ok
	}
	for name := range e {
		get(name, 0)
	}
}

// expand replaces the references of s with lookup; the second result is
// false when one cannot be resolved.
func expand(s string, lookup func(name string) (string, bool)) (string, bool) {
	ok := true
	out := reference.ReplaceAllStringFunc(s, func(m string) string {
		sub := reference.FindStringSubmatch(m)
		name, def, hasDefault := sub[1], sub[2], strings.Contains(m, ":-")
		if name == "" {
			name = sub[3]
		}
		if v, found := lookup(name); found && v != "" {
			return v
		}
		if hasDefault {
			return def
		}
		ok = false
		return m
	})
	return out, ok
}

// parseDotenv reads a .env file into e, in the Symfony Dotenv format, which
// Next.js and the other loaders share: KEY=value, "export" allowed, single
// quotes literal, double quotes with escapes and references, both possibly
// on several lines, "#" starting a comment outside quotes. An invalid line
// is reported and skipped, the rest of the file still read.
func (e env) parseDotenv(file, content string) error {
	var errs []error
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		lineNo := i + 1
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, raw, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" || strings.ContainsAny(key, " \t") {
			errs = append(errs, fmt.Errorf("%s:%d: invalid line", file, lineNo))
			continue
		}
		raw = strings.TrimLeft(raw, " \t")
		var v string
		interp := false
		switch {
		case strings.HasPrefix(raw, "'"):
			body, next := raw[1:], i
			end := strings.Index(body, "'")
			for end < 0 && next+1 < len(lines) {
				next++
				body += "\n" + lines[next]
				end = strings.Index(body, "'")
			}
			if end < 0 {
				errs = append(errs, fmt.Errorf("%s:%d: unterminated quote", file, lineNo))
				continue
			}
			v, i = body[:end], next
		case strings.HasPrefix(raw, `"`):
			body, next := raw[1:], i
			for !closesDouble(body) && next+1 < len(lines) {
				next++
				body += "\n" + lines[next]
			}
			end := closingDouble(body)
			if end < 0 {
				errs = append(errs, fmt.Errorf("%s:%d: unterminated quote", file, lineNo))
				continue
			}
			v, interp = unescape(body[:end]), true
			i = next
		default:
			if i := strings.Index(raw, " #"); i >= 0 {
				raw = raw[:i]
			}
			v, interp = strings.TrimSpace(raw), true
		}
		if interp {
			e.set(key, v, file, lineNo)
		} else {
			e[key] = value{value: v, file: file, line: lineNo}
		}
	}
	return errors.Join(errs...)
}

// closingDouble returns the index of the unescaped closing double quote.
func closingDouble(s string) int {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i
		}
	}
	return -1
}

func closesDouble(s string) bool { return closingDouble(s) >= 0 }

func unescape(s string) string {
	return strings.NewReplacer(`\"`, `"`, `\\`, `\`, `\n`, "\n", `\$`, "$").Replace(s)
}

// phpPair matches 'KEY' => 'value' (or a number, a boolean, null) in the
// array .env.local.php returns.
var phpPair = regexp.MustCompile(`(?s)(['"])((?:\\.|[^\\])*?)['"]\s*=>\s*(?:(['"])((?:\\.|[^\\])*?)['"]|([0-9.]+|true|false|null))`)

// parsePHPEnv reads the array of a .env.local.php, written by composer
// dump-env: values are literals, already interpolated.
func (e env) parsePHPEnv(file, content string) {
	for _, loc := range phpPair.FindAllStringSubmatchIndex(content, -1) {
		key := content[loc[4]:loc[5]]
		v := ""
		if loc[8] >= 0 {
			v = strings.NewReplacer(`\'`, "'", `\"`, `"`, `\\`, `\`).Replace(content[loc[8]:loc[9]])
		} else if loc[10] >= 0 {
			v = content[loc[10]:loc[11]]
		}
		e[key] = value{value: v, file: file, line: strings.Count(content[:loc[0]], "\n") + 1}
	}
}
