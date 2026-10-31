import { repositoryUrl } from './git'

test('repositoryUrl', () => {
  expect(repositoryUrl('https://git.acme.example/site/www.git')).toBe('https://git.acme.example/site/www')
  expect(repositoryUrl('https://user:secret@git.acme.example:8443/site/www')).toBe('https://git.acme.example:8443/site/www')
  expect(repositoryUrl('git@github.com:acme/shop.git')).toBe('https://github.com/acme/shop')
  expect(repositoryUrl('ssh://git@git.acme.example:2222/group/sub/repo.git')).toBe('https://git.acme.example/group/sub/repo')
  expect(repositoryUrl('git://git.acme.example/repo')).toBe('https://git.acme.example/repo')
  expect(repositoryUrl('/srv/git/repo.git')).toBeNull()
  expect(repositoryUrl('file:///srv/git/repo.git')).toBeNull()
  expect(repositoryUrl('javascript:alert(1)')).toBeNull()
  expect(repositoryUrl('https://git.acme.example')).toBeNull()
})
