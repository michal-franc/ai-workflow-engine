/** One approval the issue's current status offers (GET …/approvals.json). */
export type ApprovalItem = { status: string; approved: boolean; optional?: boolean }

/** GET /p/<project>/issue/<slug>/approvals.json on the AI Workflow Engine board. */
export type Approvals = {
  slug: string
  title: string
  status: string
  human_approval: string
  approvals: ApprovalItem[]
}

/** Where the Playtest button is in its cycle. */
export type PlaytestStep = 'idle' | 'running' | 'ran' | 'reporting'

declare module 'claude-code' {
  interface PluginState {
    'issue-approve': {
      /** The session's issue approvals; null outside a dispatched agent or when the board is down. */
      approvals: Approvals | null
      /** The status whose approval is being sent, '' when idle. */
      sending: string
      /** The worktree's Makefile has a `playtest` target (checked once at start). */
      hasPlaytest: boolean
      /** The playtest button's step: idle → running → ran (offer Report) → reporting → idle. */
      playtest: PlaytestStep
    }
  }
}
