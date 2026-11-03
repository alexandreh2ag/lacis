import { safeHttpUrl } from './url'

test.each([
  ['https://ci.example.com/acme/shop/-/pipelines/42', 'https://ci.example.com/acme/shop/-/pipelines/42'],
  ['http://ci.example.com/42', 'http://ci.example.com/42'],
  [' https://ci.example.com/42 ', 'https://ci.example.com/42'],
  ['https://bot:secret@ci.example.com/42', 'https://ci.example.com/42'],
  ['javascript:alert(1)', null],
  ['JaVaScRiPt:alert(1)', null],
  ['data:text/html,<script>alert(1)</script>', null],
  ['file:///etc/passwd', null],
  ['//evil.example.com', null],
  ['/relative', null],
  ['', null],
  [null, null],
])('safeHttpUrl(%j) is %j', (raw, want) => {
  expect(safeHttpUrl(raw)).toBe(want)
})
