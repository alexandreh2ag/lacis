import { safeRedirect } from './Login'

test.each([
  ['/acme/graph?lens=infra', '/acme/graph?lens=infra'],
  ['/', '/'],
  [null, '/'],
  ['', '/'],
  ['https://evil.example.com', '/'],
  ['//evil.example.com', '/'],
  ['/\\evil.example.com', '/'],
  ['/acme\\..\\x', '/'],
  ['/\tevil', '/'],
  ['/acme\nSet-Cookie: x', '/'],
  ['/acme\u007f', '/'],
  ['acme', '/'],
])('safeRedirect(%j) is %j', (target, want) => {
  expect(safeRedirect(target)).toBe(want)
})
