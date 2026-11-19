// The stack of an app: the distribution it is built on, such as Sulu or
// Akeneo, over its framework, such as Symfony.
export interface Stack {
  framework?: string | null
  frameworkVersion?: string | null
  distribution?: string | null
  distributionVersion?: string | null
}

// appTech is the technology an app shows the logo of: its distribution,
// else its framework.
export function appTech(a: Stack, fallback = 'app'): string {
  return a.distribution || a.framework || fallback
}

// stackText is the distribution and the framework with their versions:
// "sulu 2.6.4 · symfony 6.4.12".
export function stackText(a: Stack): string {
  return [[a.distribution, a.distributionVersion], [a.framework, a.frameworkVersion]]
    .map((parts) => parts.filter(Boolean).join(' '))
    .filter(Boolean)
    .join(' · ')
}
