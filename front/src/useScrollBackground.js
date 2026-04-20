import { useEffect } from 'react'

const SCROLL_START_OFFSET = 720
const SCROLL_RANGE = 8400
const SCROLL_EASING = 0.14
const MOON_MAX_SHIFT = 480

const backgroundStart = {
  top: [6, 9, 19],
  middle: [8, 16, 29],
  bottom: [12, 22, 38],
  leftGlowAlpha: 0.18,
  rightGlowAlpha: 0.12,
}

const backgroundEnd = {
  top: [10, 18, 34],
  middle: [14, 29, 52],
  bottom: [20, 41, 68],
  leftGlowAlpha: 0.3,
  rightGlowAlpha: 0.2,
}

function clamp(value, min, max) {
  return Math.min(Math.max(value, min), max)
}

function interpolateNumber(start, end, progress) {
  return start + (end - start) * progress
}

function interpolateColor(start, end, progress) {
  const channels = start.map((channel, index) =>
    Math.round(interpolateNumber(channel, end[index], progress)),
  )

  return `rgb(${channels.join(' ')})`
}

function applyBackgroundProgress(progress) {
  const root = document.documentElement

  root.style.setProperty(
    '--sky-top',
    interpolateColor(backgroundStart.top, backgroundEnd.top, progress),
  )
  root.style.setProperty(
    '--sky-middle',
    interpolateColor(backgroundStart.middle, backgroundEnd.middle, progress),
  )
  root.style.setProperty(
    '--sky-bottom',
    interpolateColor(backgroundStart.bottom, backgroundEnd.bottom, progress),
  )
  root.style.setProperty(
    '--sky-left-glow-alpha',
    interpolateNumber(
      backgroundStart.leftGlowAlpha,
      backgroundEnd.leftGlowAlpha,
      progress,
    ).toFixed(3),
  )
  root.style.setProperty(
    '--sky-right-glow-alpha',
    interpolateNumber(
      backgroundStart.rightGlowAlpha,
      backgroundEnd.rightGlowAlpha,
      progress,
    ).toFixed(3),
  )
  root.style.setProperty(
    '--moon-shift-y',
    `${interpolateNumber(0, MOON_MAX_SHIFT, progress).toFixed(1)}px`,
  )
}

function readScrollProgress() {
  const scrollTop = window.scrollY || window.pageYOffset || 0
  return clamp((scrollTop - SCROLL_START_OFFSET) / SCROLL_RANGE, 0, 1)
}

export default function useScrollBackground() {
  useEffect(() => {
    if (typeof window === 'undefined') {
      return undefined
    }

    const mediaQuery = window.matchMedia('(prefers-reduced-motion: reduce)')
    let animationFrameId = 0
    let currentProgress = readScrollProgress()
    let targetProgress = currentProgress

    applyBackgroundProgress(currentProgress)

    function stopAnimation() {
      if (animationFrameId !== 0) {
        window.cancelAnimationFrame(animationFrameId)
        animationFrameId = 0
      }
    }

    function animateProgress() {
      const prefersReducedMotion = mediaQuery.matches

      if (prefersReducedMotion) {
        currentProgress = targetProgress
      } else {
        currentProgress += (targetProgress - currentProgress) * SCROLL_EASING
      }

      if (Math.abs(targetProgress - currentProgress) < 0.001) {
        currentProgress = targetProgress
      }

      applyBackgroundProgress(currentProgress)

      if (currentProgress !== targetProgress && !prefersReducedMotion) {
        animationFrameId = window.requestAnimationFrame(animateProgress)
        return
      }

      animationFrameId = 0
    }

    function scheduleProgressUpdate() {
      targetProgress = readScrollProgress()

      if (animationFrameId === 0) {
        animationFrameId = window.requestAnimationFrame(animateProgress)
      }
    }

    function handleMotionPreferenceChange() {
      stopAnimation()
      currentProgress = readScrollProgress()
      targetProgress = currentProgress
      applyBackgroundProgress(currentProgress)
    }

    window.addEventListener('scroll', scheduleProgressUpdate, { passive: true })
    window.addEventListener('resize', scheduleProgressUpdate)

    if (typeof mediaQuery.addEventListener === 'function') {
      mediaQuery.addEventListener('change', handleMotionPreferenceChange)
    } else {
      mediaQuery.addListener(handleMotionPreferenceChange)
    }

    return () => {
      stopAnimation()
      window.removeEventListener('scroll', scheduleProgressUpdate)
      window.removeEventListener('resize', scheduleProgressUpdate)

      if (typeof mediaQuery.removeEventListener === 'function') {
        mediaQuery.removeEventListener('change', handleMotionPreferenceChange)
      } else {
        mediaQuery.removeListener(handleMotionPreferenceChange)
      }
    }
  }, [])
}
