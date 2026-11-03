import { pages } from './Listing'

test('pages', () => {
  expect(pages(1, 3)).toEqual([1, 2, 3])
  expect(pages(1, 9)).toEqual([1, 2, null, 9])
  expect(pages(5, 9)).toEqual([1, null, 4, 5, 6, null, 9])
  expect(pages(9, 9)).toEqual([1, null, 8, 9])
})
