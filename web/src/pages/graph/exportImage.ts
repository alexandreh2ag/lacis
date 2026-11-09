import type { Rect } from '@xyflow/react'
import { getFontEmbedCSS, toPng, toSvg } from 'html-to-image'
import { slimSvg } from './slimSvg'

export const exportFormats = ['svg', 'png', 'png-transparent'] as const
export type ExportFormat = (typeof exportFormats)[number]

// PAD is the margin around the graph: the routes of the edges go round
// the cards, past their bounds.
const PAD = 48
// MAX_SIDE keeps a PNG under the size of a canvas the browsers draw.
const MAX_SIDE = 8192

// frame is the size of the image of a graph of those bounds, the
// transform bringing the graph into it, and the pixel ratio of a PNG:
// 2 for a sharp text, less for a graph too large.
export function frame(bounds: Rect) {
  const width = Math.ceil(bounds.width + 2 * PAD)
  const height = Math.ceil(bounds.height + 2 * PAD)
  return {
    width,
    height,
    transform: `translate(${PAD - bounds.x}px, ${PAD - bounds.y}px) scale(1)`,
    ratio: Math.min(2, MAX_SIDE / Math.max(width, height)),
  }
}

// kept leaves out of the image what only helps to edit the graph: the
// resize controls of the lanes and the tooltips.
function kept(n: HTMLElement): boolean {
  const c = n.classList
  return !c || !(c.contains('react-flow__resize-control') || c.contains('lc-tooltip'))
}

// LATIN is the start of the unicode ranges of the latin and latin-ext
// subsets of the fonts, as written (U+0000-00FF) or as the browser gives
// it back (U+0-FF): the others (cyrillic, greek, vietnamese) would weigh
// in each image, for letters a graph does not show.
const LATIN = /unicode-range:\s*U\+0*(0-0*FF|100-0*2)/i

// latinFonts is the CSS of the fonts embedded in an image, read once.
let fonts: Promise<string> | null = null
export function latinOnly(css: string): string {
  return css.split(/(?=@font-face)/).filter((rule) => !rule.includes('unicode-range') || LATIN.test(rule)).join('')
}
function latinFonts(node: HTMLElement): Promise<string> {
  fonts ??= getFontEmbedCSS(node).then(latinOnly).catch((err: unknown) => {
    fonts = null
    throw err
  })
  return fonts
}

// exportImage draws the whole graph, not only what the viewport shows,
// as the user sees it: the theme, the zone opened, the impact.
export async function exportImage(root: HTMLElement, bounds: Rect, format: ExportFormat): Promise<string> {
  const viewport = root.querySelector<HTMLElement>('.react-flow__viewport')
  if (!viewport) throw new Error('no viewport')
  const f = frame(bounds)
  // Zoomed out, the edges lose their arrows and dashes: not in the image.
  const far = root.classList.contains('lc-flow--far')
  root.classList.remove('lc-flow--far')
  try {
    const options = {
      width: f.width,
      height: f.height,
      // React Flow leaves its own background transparent: the color is the
      // token of the background of the graph.
      backgroundColor: format === 'png-transparent' ? undefined : getComputedStyle(root).getPropertyValue('--surface-sunken').trim(),
      fontEmbedCSS: await latinFonts(viewport),
      style: { width: `${f.width}px`, height: `${f.height}px`, transform: f.transform },
      filter: kept,
    }
    if (format !== 'svg') return await toPng(viewport, { ...options, pixelRatio: f.ratio })
    const url = await toSvg(viewport, options)
    const comma = url.indexOf(',')
    const svg = await slimSvg(decodeURIComponent(url.slice(comma + 1)))
    return url.slice(0, comma + 1) + encodeURIComponent(svg)
  } finally {
    root.classList.toggle('lc-flow--far', far)
  }
}

// download saves the data URL as a file.
export function download(url: string, name: string) {
  const a = document.createElement('a')
  a.href = url
  a.download = name
  a.click()
}
