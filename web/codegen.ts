import type { CodegenConfig } from '@graphql-codegen/cli'

// The types of the queries, from the schema of the server.
const config: CodegenConfig = {
  schema: '../graph/schema/*.graphqls',
  documents: ['src/**/*.{ts,tsx}', '!src/graphql/**'],
  generates: {
    'src/graphql/': {
      preset: 'client',
      presetConfig: { fragmentMasking: false },
      config: {
        scalars: { ID: 'string', Time: 'string', JSON: 'unknown' },
        enumsAsTypes: true,
        useTypeImports: true,
      },
    },
  },
}

export default config
