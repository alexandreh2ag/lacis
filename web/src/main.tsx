import { ApolloProvider } from '@apollo/client'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router'
import { client } from './apollo'
import { App } from './App'
import { basename } from './config'
import { LangProvider } from './i18n/Provider'
import './styles/index.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ApolloProvider client={client}>
      <BrowserRouter basename={basename}>
        <LangProvider>
          <App />
        </LangProvider>
      </BrowserRouter>
    </ApolloProvider>
  </StrictMode>,
)
