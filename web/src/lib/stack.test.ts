import { appTech, stackText } from './stack'

test.each([
  [{ framework: 'symfony', frameworkVersion: '6.4.12', distribution: 'sulu', distributionVersion: '2.6.4' }, 'sulu', 'sulu 2.6.4 · symfony 6.4.12'],
  [{ framework: 'symfony', frameworkVersion: '7.2', distribution: 'akeneo' }, 'akeneo', 'akeneo · symfony 7.2'],
  [{ framework: 'laravel', frameworkVersion: '11.9.0', distribution: null }, 'laravel', 'laravel 11.9.0'],
  [{ framework: 'zend1' }, 'zend1', 'zend1'],
  [{}, 'app', ''],
])('the stack of %o', (a, tech, text) => {
  expect(appTech(a)).toBe(tech)
  expect(stackText(a)).toBe(text)
})

test('appTech falls back to the given technology', () => {
  expect(appTech({}, 'node')).toBe('node')
})
