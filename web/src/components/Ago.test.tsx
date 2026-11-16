import { render, screen } from '@testing-library/react'
import { formatFull } from '@/lib/format'
import { Ago } from './Ago'

describe('Ago', () => {
  const at = '2026-11-15T21:30:00Z'

  it('shows the full date and time on hover', () => {
    render(<Ago at={at} />)
    const time = screen.getByTitle(formatFull(at))
    expect(time.tagName).toBe('TIME')
    expect(time.getAttribute('datetime')).toBe(at)
    expect(time.textContent).not.toBe('')
  })

  it('keeps the sentence given as children', () => {
    render(<Ago at={at}>used yesterday</Ago>)
    expect(screen.getByTitle(formatFull(at)).textContent).toBe('used yesterday')
  })

  it('renders nothing without a date', () => {
    const { container } = render(<Ago at={null} />)
    expect(container.innerHTML).toBe('')
  })
})
