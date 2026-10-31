import { admin } from './admin'
import { common } from './common'
import { components } from './components'
import { graph } from './graph'
import { pages } from './pages'

// The French text is the reference: the other languages have its shape.
export const fr = { common, components, pages, graph, admin }

export type Messages = typeof fr
