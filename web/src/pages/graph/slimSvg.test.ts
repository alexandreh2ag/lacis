import { slimDecls, type Decl } from './slimSvg'

const decl = (name: string, value: string, priority = ''): Decl => ({ name, value, priority })

test('a style keeps what differs from its parent and the defaults', () => {
  const parent = new Map([['color', 'rgb(1, 2, 3)'], ['font-size', '13px']])
  const defaults = new Map([['opacity', '1'], ['border-top-width', '0px'], ['margin-top', '0px'], ['display', 'block']])
  const { style, inherited } = slimDecls([
    decl('--brand', '#123'),
    decl('color', 'rgb(1, 2, 3)'),
    decl('font-size', '14px'),
    decl('opacity', '1'),
    decl('margin-top', '4px', 'important'),
    decl('border-top-width', '0px'),
    decl('display', 'block'),
  ], parent, defaults)
  expect(style).toBe('font-size: 14px; margin-top: 4px !important; border-top-width: 0px; display: block')
  expect(inherited.get('font-size')).toBe('14px')
  expect(inherited.get('color')).toBe('rgb(1, 2, 3)')
  expect(parent.get('font-size')).toBe('13px')
})
