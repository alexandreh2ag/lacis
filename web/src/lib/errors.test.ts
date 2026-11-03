import { errorMessage, reasonText, refusalOf } from './errors'

const gql = (extensions: unknown, message = 'refused') => ({ graphQLErrors: [{ message, extensions }] })

test('refusalOf reads the code, the reason and the args of the first error', () => {
  expect(refusalOf(gql({ code: 'NOT_FOUND', reason: 'HOST_NOT_FOUND', args: { hostname: 'web-9' } })))
    .toEqual({ message: 'refused', code: 'NOT_FOUND', reason: 'HOST_NOT_FOUND', args: { hostname: 'web-9' } })
  expect(refusalOf(gql({ code: 1, reason: null, args: 'x' }))).toEqual({ message: 'refused', code: undefined, reason: undefined, args: {} })
  expect(refusalOf(gql(undefined))).toEqual({ message: 'refused', code: undefined, reason: undefined, args: {} })
  expect(refusalOf(new Error('down'))).toBeNull()
})

test('an unknown reason falls back on the code, then on the message', () => {
  expect(reasonText({ message: '', code: 'NOT_FOUND', reason: 'SOMETHING_NEW', args: {} })).toContain('n\'existe plus')
  expect(errorMessage(gql({ code: 'CONFLICT', reason: 'SOMETHING_NEW' }, 'already exists: thing'))).toBe('thing')
  expect(errorMessage(gql({ code: 'BAD_USER_INPUT', reason: 'NAME_TOO_LONG', args: { max: 100 } }))).toBe('Un nom fait au plus 100 caractères.')
  expect(errorMessage(new Error('boom'))).toBe('boom')
})
