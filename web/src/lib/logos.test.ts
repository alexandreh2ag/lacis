import { defaultTechs, logoOf, techOfDomain } from './logos'

test('Vault, Varnish, S3, MinIO and Silo have a logo', () => {
  for (const tech of ['vault', 'varnish', 's3', 'Varnish', 'minio', 'silo']) expect(logoOf(tech)?.path).toBeTruthy()
  expect(defaultTechs).toEqual(expect.arrayContaining(['minio', 's3', 'silo', 'varnish', 'vault']))
})

test.each([
  ['s3.amazonaws.com', 's3'],
  ['acme-assets.s3.amazonaws.com', 's3'],
  ['acme-assets.s3.eu-west-3.amazonaws.com', 's3'],
  ['s3-eu-west-1.amazonaws.com', 's3'],
  ['s3.gra.io.cloud.ovh.net', 's3'],
  ['acme.s3.gra.io.cloud.ovh.net', 's3'],
  ['s3.fr-par.scw.cloud', 's3'],
  ['acme.s3.fr-par.scw.cloud', 's3'],
  ['o42.ingest.sentry.io', 'sentry'],
  ['ec2.eu-west-3.amazonaws.com', undefined],
  ['s3.example.com', undefined],
  ['nots3.amazonaws.com', undefined],
])('techOfDomain(%s) is %s', (domain, tech) => {
  expect(techOfDomain(domain)).toBe(tech)
})
