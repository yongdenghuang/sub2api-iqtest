/** Extract a complete SVG, excluding Markdown fences or surrounding prose.
 * Render only through an img data URL: model output must never enter the page
 * as HTML, an object, or an iframe with executable document content.
 */
export function svgPreviewSource(output: string): string {
  const start = output.search(/<svg(?=[\s>])/i)
  if (start < 0) return ''
  const candidate = output.slice(start)
  // Try closing tags in order so nested SVGs and multiple fenced documents do
  // not include trailing prose or silently display a different test result.
  for (const close of candidate.matchAll(/<\/svg\s*>/gi)) {
    const svg = candidate.slice(0, close.index! + close[0].length)
    const doc = new DOMParser().parseFromString(svg, 'image/svg+xml')
    if (doc.querySelector('parsererror') || doc.documentElement.localName !== 'svg') continue
    if (!doc.documentElement.namespaceURI) {
      doc.documentElement.setAttribute('xmlns', 'http://www.w3.org/2000/svg')
    } else if (doc.documentElement.namespaceURI !== 'http://www.w3.org/2000/svg') {
      return ''
    }
    return 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(new XMLSerializer().serializeToString(doc))
  }
  return ''
}
