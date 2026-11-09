import { frame, latinOnly } from './exportImage'

test('the image holds the graph and its margin, sharp unless too large', () => {
  expect(frame({ x: -100, y: 20, width: 1000, height: 500 })).toEqual({
    width: 1096, height: 596, transform: 'translate(148px, 28px) scale(1)', ratio: 2,
  })
  expect(frame({ x: 0, y: 0, width: 16288, height: 400 }).ratio).toBeCloseTo(0.5)
})

test('the fonts embedded keep the latin subsets only', () => {
  const face = (range: string) => `@font-face { font-family: a; src: url(x); unicode-range: ${range}; }`
  const css = face('U+0460-052F') + face('U+0000-00FF,U+0131') + face('U+0100-02BA,U+02BD') + face('U+1F00-1FFF') + face('U+0-FF, U+131')
    + face('U+100-2BA') + face('U+102-103') + '@font-face { font-family: b; src: url(y); }'
  expect(latinOnly(css)).toBe(face('U+0000-00FF,U+0131') + face('U+0100-02BA,U+02BD') + face('U+0-FF, U+131') + face('U+100-2BA')
    + '@font-face { font-family: b; src: url(y); }')
})
