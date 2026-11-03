import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'
import { pick } from '@/i18n'

// The tests read the French text.
pick('fr')

afterEach(() => cleanup())
