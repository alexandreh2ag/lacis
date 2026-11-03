import { ApolloClient, HttpLink, InMemoryCache, from } from '@apollo/client'
import { onError } from '@apollo/client/link/error'

// unauthorized is called when the session is gone: the API answers 401.
let unauthorized: () => void = () => {}
export function onUnauthorized(fn: () => void) {
  unauthorized = fn
}

const errors = onError(({ networkError }) => {
  if (networkError && 'statusCode' in networkError && networkError.statusCode === 401) {
    unauthorized()
  }
})

// The URI is relative: the <base> of the page carries the base path.
const http = new HttpLink({ uri: 'graphql', credentials: 'same-origin' })

export const client = new ApolloClient({
  link: from([errors, http]),
  cache: new InMemoryCache({
    typePolicies: {
      // Ids are unique per type; namespaces are known by slug too.
      Namespace: { keyFields: ['id'] },
      // The graph of a level is no entity: keep it with its query.
      Graph: { keyFields: false },
      GraphNode: { keyFields: false },
      GraphEdge: { keyFields: false },
    },
  }),
  defaultOptions: { watchQuery: { fetchPolicy: 'cache-and-network' } },
})
