import { gql } from '@apollo/client'
import { describe, expect, it } from 'vitest'
import { client } from './apollo'

const hosts = gql`
  query CacheHosts {
    hosts { id os { id pretty } }
  }
`

const impact = gql`
  query CacheImpact($target: String!) {
    impact(target: $target) { id level }
  }
`

describe('apollo cache', () => {
  it.each([
    {
      name: 'keeps the OS of each host when they share a distribution',
      query: hosts,
      variables: {},
      data: {
        hosts: [
          { __typename: 'Host', id: 'h1', os: { __typename: 'OS', id: 'debian', pretty: 'Debian GNU/Linux 11 (bullseye)' } },
          { __typename: 'Host', id: 'h2', os: { __typename: 'OS', id: 'debian', pretty: 'Debian GNU/Linux 13 (trixie)' } },
        ],
      },
    },
    {
      name: 'keeps the level of an impact node with its target',
      query: impact,
      variables: { target: 'a' },
      data: { impact: [{ __typename: 'GraphImpactNode', id: 'n1', level: 1 }] },
      other: { variables: { target: 'b' }, data: { impact: [{ __typename: 'GraphImpactNode', id: 'n1', level: 2 }] } },
    },
  ])('$name', ({ query, variables, data, other }) => {
    const cache = client.cache
    cache.reset()
    cache.writeQuery({ query, variables, data })
    if (other) cache.writeQuery({ query, ...other })
    expect(cache.readQuery({ query, variables })).toEqual(data)
    if (other) expect(cache.readQuery({ query, variables: other.variables })).toEqual(other.data)
  })
})
