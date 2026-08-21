let lockCount = 0
let priorBodyOverflow = ''
let priorHtmlOverflow = ''

/** Reference-counted scroll lock for overlays. Restores overflow when the last lock releases. */
export function lockScroll() {
  if (lockCount === 0) {
    priorBodyOverflow = document.body.style.overflow
    priorHtmlOverflow = document.documentElement.style.overflow
    document.body.style.overflow = 'hidden'
    document.documentElement.style.overflow = 'hidden'
  }
  lockCount += 1
  return () => {
    lockCount = Math.max(0, lockCount - 1)
    if (lockCount === 0) {
      document.body.style.overflow = priorBodyOverflow
      document.documentElement.style.overflow = priorHtmlOverflow
    }
  }
}
