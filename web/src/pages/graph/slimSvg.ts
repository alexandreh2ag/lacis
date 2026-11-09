// html-to-image copies on each element of the SVG its whole computed
// style: some 400 properties, the variables of the theme among them, 10 kB
// an element and megabytes for a graph. slimSvg keeps of them only what
// the image needs, the drawing unchanged.

// INHERITED are the properties an element takes from its parent: one
// equal to the parent's is left to the inheritance.
const INHERITED = new Set([
  'border-collapse', 'border-spacing', 'caption-side', 'caret-color', 'accent-color', 'clip-rule', 'color', 'color-interpolation',
  'color-interpolation-filters', 'color-rendering', 'color-scheme', 'cursor', 'direction', 'dominant-baseline', 'dynamic-range-limit', 'empty-cells',
  'fill', 'fill-opacity', 'fill-rule', 'font-family', 'font-feature-settings', 'font-kerning', 'font-language-override', 'font-optical-sizing',
  'font-palette', 'font-size', 'font-size-adjust', 'font-stretch', 'font-style', 'font-synthesis-small-caps', 'font-synthesis-style',
  'font-synthesis-weight', 'font-variant-alternates', 'font-variant-caps', 'font-variant-east-asian', 'font-variant-emoji',
  'font-variant-ligatures', 'font-variant-numeric', 'font-variant-position', 'font-variation-settings', 'font-weight', 'forced-color-adjust',
  'hyphenate-character', 'hyphenate-limit-chars', 'hyphens', 'image-orientation', 'image-rendering', 'interpolate-size', 'letter-spacing',
  'line-break', 'line-height', 'list-style-image', 'list-style-position', 'list-style-type', 'marker-end', 'marker-mid', 'marker-start',
  'math-depth', 'math-shift', 'math-style', 'orphans', 'overflow-wrap', 'paint-order', 'pointer-events', 'print-color-adjust', 'quotes',
  'ruby-align', 'ruby-position', 'shape-rendering', 'stroke', 'stroke-dasharray', 'stroke-dashoffset', 'stroke-linecap', 'stroke-linejoin',
  'stroke-miterlimit', 'stroke-opacity', 'stroke-width', 'tab-size', 'text-align', 'text-align-last', 'text-anchor', 'text-autospace',
  'text-combine-upright', 'text-decoration-skip-ink', 'text-emphasis-color', 'text-emphasis-position', 'text-emphasis-style', 'text-indent',
  'text-justify', 'text-orientation', 'text-rendering', 'text-shadow', 'text-size-adjust', 'text-spacing-trim', 'text-transform',
  'text-underline-offset', 'text-underline-position', 'text-wrap-mode', 'text-wrap-style', 'user-select', 'visibility',
  'white-space-collapse', 'widows', 'word-break', 'word-spacing', 'writing-mode', '-webkit-border-horizontal-spacing',
  '-webkit-border-vertical-spacing', '-webkit-font-smoothing', '-webkit-locale', '-webkit-rtl-ordering', '-webkit-tap-highlight-color',
  '-webkit-text-emphasis-color', '-webkit-text-emphasis-position', '-webkit-text-emphasis-style', '-webkit-text-fill-color',
  '-webkit-text-security', '-webkit-text-stroke-color', '-webkit-text-stroke-width', '-webkit-user-modify', '-moz-osx-font-smoothing',
])

// KEPT are the properties whose computed value hangs on others: a border
// width is 0 while its style is none, but not once the style is solid.
// They stay even when they equal the default.
const KEPT = /(border|outline|column-rule).*width|^(width|height|display|position|top|right|bottom|left|inset.*)$/

export type Decl = { name: string; value: string; priority: string }

// slimDecls keeps of the declarations of an element those the image
// needs: not the variables, already resolved in the values; not an
// inherited property equal to the parent's; not another equal to the
// default of the element. It gives the inherited values for its children.
export function slimDecls(decls: Decl[], parent: Map<string, string>, defaults: Map<string, string>) {
  const inherited = new Map(parent)
  const kept: string[] = []
  for (const d of decls) {
    if (d.name.startsWith('--')) continue
    if (INHERITED.has(d.name)) {
      inherited.set(d.name, d.value)
      if (parent.get(d.name) === d.value) continue
    } else if (!KEPT.test(d.name) && defaults.get(d.name) === d.value) continue
    kept.push(`${d.name}: ${d.value}${d.priority ? ' !important' : ''}`)
  }
  return { style: kept.join('; '), inherited }
}

const SVG_NS = 'http://www.w3.org/2000/svg'

// blankDocument is a page without the CSS of Lacis, where an element shows
// the defaults of the browser: those of the elements of the image.
function blankDocument(): Promise<{ doc: Document; done: () => void }> {
  return new Promise((resolve, reject) => {
    const frame = document.createElement('iframe')
    frame.style.display = 'none'
    frame.srcdoc = '<!doctype html><html><body></body></html>'
    frame.onload = () => resolve({ doc: frame.contentDocument!, done: () => frame.remove() })
    frame.onerror = () => reject(new Error('no blank document'))
    document.body.appendChild(frame)
  })
}

// slimSvg slims the styles of an SVG made by html-to-image.
export async function slimSvg(svg: string): Promise<string> {
  const { doc: blank, done } = await blankDocument()
  try {
    const defaults = new Map<string, Map<string, string>>()
    const defaultsOf = (el: Element) => {
      const key = `${el.namespaceURI}|${el.localName}`
      let m = defaults.get(key)
      if (!m) {
        const b = blank.createElementNS(el.namespaceURI, el.localName)
        if (el.namespaceURI === SVG_NS && el.localName !== 'svg') blank.body.appendChild(blank.createElementNS(SVG_NS, 'svg')).appendChild(b)
        else blank.body.appendChild(b)
        const cs = blank.defaultView!.getComputedStyle(b)
        m = new Map(Array.from(cs, (n) => [n, cs.getPropertyValue(n)]))
        defaults.set(key, m)
      }
      return m
    }
    const doc = new DOMParser().parseFromString(svg, 'image/svg+xml')
    const walk = (el: Element, parent: Map<string, string>) => {
      let inherited = parent
      const style = (el as HTMLElement).style as CSSStyleDeclaration | undefined
      if (style && el.hasAttribute('style')) {
        const decls = Array.from(style, (name) => ({ name, value: style.getPropertyValue(name), priority: style.getPropertyPriority(name) }))
        const slim = slimDecls(decls, parent, defaultsOf(el))
        el.setAttribute('style', slim.style)
        inherited = slim.inherited
      }
      for (const c of el.children) walk(c, inherited)
    }
    walk(doc.documentElement, new Map())
    return new XMLSerializer().serializeToString(doc)
  } finally {
    done()
  }
}
