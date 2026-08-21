import type { Simulation } from 'd3-force'
import type { GraphLayoutProfile, LayoutLink, LayoutNode } from './graphLayout'

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

export type SimulationRunner = {
  stop: () => void
  restart: (alpha?: number) => void
  setAlphaTarget: (target: number) => void
}

function now() {
  return typeof performance !== 'undefined' ? performance.now() : Date.now()
}

export function runSteppedSimulation(
  simulation: Simulation<LayoutNode, LayoutLink>,
  layout: GraphLayoutProfile,
  onTick: () => void,
  onEnd: () => void,
): SimulationRunner {
  let timer = 0
  let stopped = false
  simulation.stop()

  const shouldContinue = () => {
    if (stopped) return false
    if ((simulation.alphaTarget() ?? 0) > 0) return true
    return simulation.alpha() > simulation.alphaMin()
  }

  const pump = () => {
    if (stopped) return
    const started = now()
    let ticks = 0
    while (shouldContinue() && ticks < layout.tickBatch && now() - started < layout.tickBudgetMs) {
      simulation.tick()
      ticks += 1
    }
    if (ticks === 0 && shouldContinue()) simulation.tick()
    onTick()
    if (!shouldContinue()) {
      onEnd()
      return
    }
    timer = setTimeout(pump, (simulation.alphaTarget() ?? 0) > 0 ? Math.max(layout.tickYieldMs, 8) : layout.tickYieldMs) as unknown as number
  }

  const startPump = () => {
    if (stopped) return
    clearTimeout(timer)
    pump()
  }

  return {
    stop() {
      stopped = true
      clearTimeout(timer)
      simulation.stop()
    },
    restart(alpha) {
      stopped = false
      if (alpha != null) simulation.alpha(alpha)
      startPump()
    },
    setAlphaTarget(target) {
      simulation.alphaTarget(target)
      if (target > 0) {
        stopped = false
        startPump()
      }
    },
  }
}
