import { graphql } from '@/graphql'
import type { GraphViewQuery } from '@/graphql/graphql'

export const GraphViewDoc = graphql(`
  query GraphView($ns: String!, $level: GraphLevel!, $filter: GraphFilter) {
    namespace(slug: $ns) {
      id
      slug
      environments { id slug name sortOrder }
      projects { id slug name }
      graph(level: $level, filter: $filter) {
        level
        nodes {
          id kind label detail hostname project env role version techs count apps members selected status expanded
          divergences { app hosts { hostname version commit } }
          hosts { id hostname status lastSeenAt agentVersion os { pretty } ips { address defaultRoute } }
          entity {
            __typename
            ... on App { id name path framework frameworkVersion vhosts code { version commit branch tag } runtime { type version } }
            ... on Service { id type instance version }
            ... on Webserver { id type version domains }
            ... on Host { id hostname }
          }
        }
        edges { id from to source confidence rule labels count crossEnv }
        groups { id label project env nodes }
      }
    }
  }
`)

export const HostnamesDoc = graphql(`
  query Hostnames($ns: String!) {
    namespace(slug: $ns) { id hosts(page: { limit: 500 }) { items { id hostname } } }
  }
`)

type Graph = NonNullable<GraphViewQuery['namespace']>['graph']
export type GNode = Graph['nodes'][number]
export type GEdge = Graph['edges'][number]
export type GGroup = Graph['groups'][number]
export type GraphData = Graph

export const ImpactDoc = graphql(`
  query GraphImpact($ns: String!, $level: GraphLevel!, $filter: GraphFilter, $node: String!, $direction: ImpactDirection!) {
    namespace(slug: $ns) {
      id
      impact(level: $level, filter: $filter, node: $node, direction: $direction) { nodes { id level } edges }
    }
  }
`)
