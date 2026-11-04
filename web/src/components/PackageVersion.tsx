import { useT } from '@/i18n'
import { Copyable } from '@/components/Copy'

// PackageVersion shows the version of the system package of a runtime or a
// service, revision included (8.1.34-9+deb.sury.org+1), when it tells more
// than the version: distributions patch an end-of-life version there.
export function PackageVersion({ version, packageVersion }: { version?: string | null; packageVersion?: string | null }) {
  const ts = useT().pages.shared
  if (!packageVersion || packageVersion === version) return null
  return (
    <span className="inline-flex items-center gap-1 text-[11.5px] text-ink-muted" title={ts.packageVersionHint}>
      {ts.packageVersion}
      <Copyable value={packageVersion} />
    </span>
  )
}
