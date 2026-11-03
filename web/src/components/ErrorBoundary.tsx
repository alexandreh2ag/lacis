import { Component, type ErrorInfo, type ReactNode } from 'react'
import { useLocation } from 'react-router'
import { useT } from '@/i18n'

class Boundary extends Component<{ fallback: (retry: () => void) => ReactNode; children: ReactNode }, { failed: boolean }> {
  state = { failed: false }

  static getDerivedStateFromError() {
    return { failed: true }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error(error, info.componentStack)
  }

  render() {
    return this.state.failed ? this.props.fallback(() => this.setState({ failed: false })) : this.props.children
  }
}

function Crash({ retry }: { retry: () => void }) {
  const t = useT().components.crash
  return (
    <div className="flex flex-col items-start gap-3 p-8" role="alert">
      <span className="font-semibold">{t.title}</span>
      <span className="lc-muted">{t.text}</span>
      <button className="lc-btn" type="button" onClick={retry}>{t.retry}</button>
    </div>
  )
}

// RouteBoundary keeps a page that fails to render, such as on data of an
// unexpected shape, from blanking the whole front: the page says so, the
// menu stays, and another page renders again.
export function RouteBoundary({ children }: { children: ReactNode }) {
  const { pathname } = useLocation()
  return <Boundary key={pathname} fallback={(retry) => <Crash retry={retry} />}>{children}</Boundary>
}
