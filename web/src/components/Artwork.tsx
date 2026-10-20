import { useState } from 'react'
import { artColor } from '../lib/format'

/** Square artwork with a coloured placeholder when there is none or it fails. */
export function Artwork({ src, title, size = 'md', className = '', rounded = true }: { src?: string; title: string; size?: 'xs' | 'sm' | 'md' | 'lg' | 'xl' | 'fill'; className?: string; rounded?: boolean }) {
  const [broken, setBroken] = useState(false)
  const cls = `art ${size} ${rounded ? '' : 'square'} ${className}`
  if (!src || broken) {
    return <span className={`${cls} ph`} style={{ background: artColor(title) }} aria-hidden><span>{title.slice(0, 1).toUpperCase()}</span></span>
  }
  return <img className={cls} src={src} alt="" loading="lazy" onError={() => setBroken(true)} />
}
