// Package icon checks an uploaded logo of a technology: its format, read
// from its content and matching its extension, its weight and, for a
// raster image, its sides. An SVG is refused when it could run a script or
// load anything.
package icon

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	// The raster formats accepted register their decoder.
	_ "image/jpeg"
	_ "image/png"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
)

// Path prefixes the path of the logo of a technology, from the base path:
// icons/{tech}.
const Path = "icons/"

// Version tells the contents of the logo of a technology apart, by the
// time it was uploaded.
func Version(updatedAt time.Time) string {
	return strconv.FormatInt(updatedAt.UnixMilli(), 10)
}

// URL is where the logo of the technology is served, its version in the
// query: a new upload has a new URL, so that the logo is cached for good.
func URL(tech string, updatedAt time.Time) string {
	return Path + tech + "?v=" + Version(updatedAt)
}

var (
	ErrTooBig    = errors.New("the logo is too big")
	ErrFormat    = errors.New("the logo is no PNG, JPEG, WebP or SVG image")
	ErrDimension = errors.New("the logo has the wrong size")
	ErrUnsafe    = errors.New("the SVG logo holds scripts or external content")
)

// Limits bound a logo.
type Limits struct {
	// MaxSize is the most bytes it weighs.
	MaxSize int64
	// MinSide and MaxSide bound the width and the height of a raster image,
	// in pixels.
	MinSide int
	MaxSide int
}

// Image is a logo checked: its media type, and the sides of a raster
// image, zero for an SVG.
type Image struct {
	ContentType string
	Width       int
	Height      int
}

const ContentTypeSVG = "image/svg+xml"

// formats are the media types accepted, by the name image.DecodeConfig
// gives the format, with their extensions.
var formats = map[string]struct {
	contentType string
	extensions  []string
}{
	"png":  {"image/png", []string{".png"}},
	"jpeg": {"image/jpeg", []string{".jpg", ".jpeg"}},
	"webp": {"image/webp", []string{".webp"}},
	"svg":  {ContentTypeSVG, []string{".svg"}},
}

// Extensions are the file extensions accepted.
func Extensions() []string {
	var out []string
	for _, f := range formats {
		out = append(out, f.extensions...)
	}
	slices.Sort(out)
	return out
}

// Check checks the logo of the file name and returns what it is.
func Check(name string, data []byte, l Limits) (*Image, error) {
	if int64(len(data)) > l.MaxSize {
		return nil, fmt.Errorf("%w: %d bytes at most", ErrTooBig, l.MaxSize)
	}
	ext := strings.ToLower(filepath.Ext(name))
	if cfg, format, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		f := formats[format]
		if f.contentType == "" {
			return nil, fmt.Errorf("%w: %s", ErrFormat, format)
		}
		if !slices.Contains(f.extensions, ext) {
			return nil, fmt.Errorf("%w: a %s image named %q", ErrFormat, format, filepath.Base(name))
		}
		if cfg.Width < l.MinSide || cfg.Height < l.MinSide || cfg.Width > l.MaxSide || cfg.Height > l.MaxSide {
			return nil, fmt.Errorf("%w: %d×%d pixels, each side between %d and %d", ErrDimension, cfg.Width, cfg.Height, l.MinSide, l.MaxSide)
		}
		return &Image{ContentType: f.contentType, Width: cfg.Width, Height: cfg.Height}, nil
	}
	if ext != ".svg" {
		return nil, fmt.Errorf("%w: %q", ErrFormat, filepath.Base(name))
	}
	if err := checkSVG(data); err != nil {
		return nil, err
	}
	return &Image{ContentType: ContentTypeSVG}, nil
}

// svgElements are the SVG elements a logo may hold, lowercased: shapes,
// text, paint servers and filters. Nothing runs a script, embeds a document
// or animates an attribute (an animation can turn a link into a script).
var svgElements = words(`svg g defs symbol use title desc metadata switch
	path rect circle ellipse line polyline polygon text tspan textpath
	lineargradient radialgradient stop pattern clippath mask marker image style
	filter feblend fecolormatrix fecomponenttransfer fecomposite feconvolvematrix
	fediffuselighting fedisplacementmap fedistantlight fedropshadow feflood
	fefunca fefuncb fefuncg fefuncr fegaussianblur femerge femergenode
	femorphology feoffset fepointlight fespecularlighting fespotlight fetile
	feturbulence`)

// svgAttributes are the attributes of no namespace a logo may hold,
// lowercased: geometry, presentation, paint and filter parameters. No
// event handler.
var svgAttributes = words(`id class style transform version baseprofile
	x y x1 y1 x2 y2 cx cy r rx ry fx fy fr dx dy z width height viewbox
	preserveaspectratio d points pathlength href
	fill fill-opacity fill-rule stroke stroke-width stroke-linecap
	stroke-linejoin stroke-miterlimit stroke-dasharray stroke-dashoffset
	stroke-opacity opacity color display visibility overflow clip-path clip-rule
	mask filter paint-order vector-effect shape-rendering image-rendering
	text-rendering color-interpolation color-interpolation-filters
	enable-background mix-blend-mode isolation
	offset stop-color stop-opacity gradientunits gradienttransform spreadmethod
	patternunits patterncontentunits patterntransform clippathunits maskunits
	maskcontentunits markerwidth markerheight markerunits refx refy orient
	marker-start marker-mid marker-end
	font-family font-size font-weight font-style font-variant text-anchor
	dominant-baseline alignment-baseline baseline-shift letter-spacing
	word-spacing text-decoration writing-mode lengthadjust textlength
	startoffset rotate method spacing side
	filterunits primitiveunits in in2 result stddeviation mode operator k1 k2 k3
	k4 values type tablevalues slope intercept amplitude exponent order
	kernelmatrix kernelunitlength divisor bias targetx targety edgemode
	preservealpha scale xchannelselector ychannelselector radius basefrequency
	numoctaves seed stitchtiles surfacescale diffuseconstant specularconstant
	specularexponent lighting-color flood-color flood-opacity azimuth elevation
	pointsatx pointsaty pointsatz limitingconeangle
	systemlanguage requiredfeatures requiredextensions
	role focusable aria-hidden aria-label aria-labelledby aria-describedby`)

// Namespaces of an SVG document.
const (
	nsSVG   = "http://www.w3.org/2000/svg"
	nsXLink = "http://www.w3.org/1999/xlink"
	nsXML   = "http://www.w3.org/XML/1998/namespace"
)

// editorNamespaces are those an editor (Inkscape, a metadata block) writes
// its data in: no browser acts on them.
var editorNamespaces = []string{
	"http://www.inkscape.org/namespaces/inkscape",
	"http://sodipodi.sourceforge.net/DTD/sodipodi-0.dtd",
	"http://www.w3.org/1999/02/22-rdf-syntax-ns#",
	"http://creativecommons.org/ns#",
	"http://purl.org/dc/elements/1.1/",
	"http://purl.org/dc/terms/",
}

func words(s string) []string {
	return strings.Fields(s)
}

// allowedElement tells an element of the allowlist, or of an editor.
func allowedElement(n xml.Name) bool {
	switch n.Space {
	case "", nsSVG:
		return slices.Contains(svgElements, strings.ToLower(n.Local))
	default:
		return slices.Contains(editorNamespaces, n.Space)
	}
}

// allowedAttribute tells an attribute of the allowlist, a namespace
// declaration, or an attribute of xlink, xml or an editor.
func allowedAttribute(n xml.Name) bool {
	local := strings.ToLower(n.Local)
	switch n.Space {
	case "":
		return local == "xmlns" || strings.HasPrefix(local, "data-") || slices.Contains(svgAttributes, local)
	case "xmlns":
		return true
	case nsXLink:
		return local == "href" || local == "title"
	case nsXML:
		return local == "space" || local == "lang"
	default:
		return slices.Contains(editorNamespaces, n.Space)
	}
}

// checkSVG reads the SVG document: an svg root, no DTD, only the elements
// and attributes of the allowlists, no link out of the document but to an
// embedded image.
func checkSVG(data []byte) error {
	d := xml.NewDecoder(bytes.NewReader(data))
	root := true
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: unreadable SVG: %v", ErrFormat, err)
		}
		switch t := tok.(type) {
		case xml.Directive:
			return fmt.Errorf("%w: a DTD", ErrUnsafe)
		case xml.ProcInst:
			if t.Target != "xml" {
				return fmt.Errorf("%w: a processing instruction %q", ErrUnsafe, t.Target)
			}
		case xml.StartElement:
			name := strings.ToLower(t.Name.Local)
			if root {
				if name != "svg" {
					return fmt.Errorf("%w: the root is <%s>, not <svg>", ErrFormat, t.Name.Local)
				}
				root = false
			}
			if !allowedElement(t.Name) {
				return fmt.Errorf("%w: a <%s> element", ErrUnsafe, t.Name.Local)
			}
			for _, a := range t.Attr {
				attr := strings.ToLower(a.Name.Local)
				value := strings.ToLower(strings.TrimSpace(a.Value))
				switch {
				case strings.HasPrefix(attr, "on"):
					return fmt.Errorf("%w: an event handler %s", ErrUnsafe, a.Name.Local)
				case !allowedAttribute(a.Name):
					return fmt.Errorf("%w: an attribute %s", ErrUnsafe, a.Name.Local)
				case attr == "href" && !strings.HasPrefix(value, "#") && !strings.HasPrefix(value, "data:image/"):
					return fmt.Errorf("%w: a link to %q", ErrUnsafe, a.Value)
				case strings.Contains(value, "javascript:") || strings.Contains(value, "url(http"):
					return fmt.Errorf("%w: %s=%q", ErrUnsafe, a.Name.Local, a.Value)
				}
			}
		case xml.CharData:
			// A style sheet may load fonts or images from elsewhere.
			if s := strings.ToLower(string(t)); strings.Contains(s, "@import") || strings.Contains(s, "url(http") || strings.Contains(s, "javascript:") {
				return fmt.Errorf("%w: a style loading external content", ErrUnsafe)
			}
		}
	}
	if root {
		return fmt.Errorf("%w: an empty document", ErrFormat)
	}
	return nil
}
