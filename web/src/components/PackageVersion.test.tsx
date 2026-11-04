import { render, screen } from '@testing-library/react'
import { PackageVersion } from './PackageVersion'

test('the package version shows when it tells more than the version', () => {
  render(<PackageVersion version="8.1.34" packageVersion="8.1.34-9+deb.sury.org+1" />)
  expect(screen.getByText('8.1.34-9+deb.sury.org+1')).toBeTruthy()
  expect(screen.getByText('paquet')).toBeTruthy()
})

test('nothing shows without package or when it is the version', () => {
  const { container, rerender } = render(<PackageVersion version="8.1.34" />)
  expect(container.textContent).toBe('')
  rerender(<PackageVersion version="24.8.4.13" packageVersion="24.8.4.13" />)
  expect(container.textContent).toBe('')
})
