import { useCallback, useEffect, useRef, useState } from 'react'

/** A horizontal row without a scrollbar: ‹ › buttons appear on hover when
 *  there is more to see, and the side that continues fades out. Swipe,
 *  trackpad and shift+wheel scrolling keep working. */
export function Carousel({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  const track = useRef<HTMLDivElement>(null)
  const [edges, setEdges] = useState({ left: false, right: false })
  const measure = useCallback(() => {
    const el = track.current
    if (!el) return
    setEdges({ left: el.scrollLeft > 4, right: el.scrollLeft + el.clientWidth < el.scrollWidth - 4 })
  }, [])
  useEffect(() => {
    measure()
    const el = track.current
    if (!el) return
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    el.addEventListener('scroll', measure, { passive: true })
    return () => { ro.disconnect(); el.removeEventListener('scroll', measure) }
  }, [measure, children])
  const page = (dir: 1 | -1) => {
    const el = track.current
    if (el) el.scrollBy({ left: dir * Math.max(200, el.clientWidth * 0.8), behavior: 'smooth' })
  }
  return (
    <div className={`carousel ${edges.left ? 'more-left' : ''} ${edges.right ? 'more-right' : ''}`}>
      <div className={`carousel-track ${className}`} ref={track}>{children}</div>
      {edges.left && <button className="carousel-btn left" onClick={() => page(-1)} aria-label="Scroll left">‹</button>}
      {edges.right && <button className="carousel-btn right" onClick={() => page(1)} aria-label="Scroll right">›</button>}
    </div>
  )
}
