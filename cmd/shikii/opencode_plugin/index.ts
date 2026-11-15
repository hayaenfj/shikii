// shikii-managed OpenCode plugin. Written by `shikii hook install`; do not edit (a rerun rewrites it, `shikii hook uninstall` removes it).
//
// OpenCode 2.x has no JSON hook settings, so this plugin calls the same `shikii hook <event>` commands Claude Code and Codex call,
// with the same JSON on stdin:
//   - the person's request          -> shikii hook user-prompt-submit (its additionalContext goes into the model's system text)
//   - the first request of a session, and the first after a summary of the conversation (compaction) -> shikii hook session-start
//   - the end of a turn             -> shikii hook stop (a block makes OpenCode continue the turn with the reason)
// A turn is what this plugin saw start: the person's request, a continuation it sent itself, or a subagent's report that wakes the
// session. An execution that no turn of those started (a summary the person asked for) runs no stop. One OpenCode server runs a plugin
// instance per project folder, and every instance is told every session's events: an instance acts only on sessions of its own folder.
// Text put into the system text is not kept in the conversation, so the plugin puts the session's brief and the request's text into
// every model call of the turn. Every hook is wrapped in try/catch and a failure or a timeout adds nothing: a hook must never stop
// the work (a throw in the prompt or context hook would make OpenCode refuse the request).
import { Plugin } from "@opencode/plugin"
import { spawn } from "node:child_process"

// The absolute path of the shikii binary (replaced when shikii writes this file). It is run directly, not through a shell.
const SHIKII: string = "__SHIKII_BIN__"
const TIMEOUT_MS: Record<string, number> = { "user-prompt-submit": 15_000, "session-start": 15_000, stop: 20_000 }
const CONTINUE_KEY = "stop-continue" // metadata of a turn this plugin started: the next stop is stop_hook_active
const CONTINUE_MAX = 8 // a safety net: after this many continuations in a row for one request, the plugin stops continuing

// run starts `shikii hook <event>` in the project's folder (cwd; as Claude Code and Codex run their hooks), passes input as JSON on
// stdin and returns the JSON it prints (undefined when it prints none, fails or times out).
function run(event: string, input: Record<string, unknown>, cwd?: string): Promise<any> {
  return new Promise((resolve) => {
    try {
      const options: Record<string, unknown> = { stdio: ["pipe", "pipe", "ignore"] }
      if (cwd) options.cwd = cwd
      const child = spawn(SHIKII, ["hook", event], options as any)
      let out = ""
      const timer = setTimeout(() => {
        try { child.kill() } catch {}
        resolve(undefined)
      }, TIMEOUT_MS[event] ?? 15_000)
      child.stdout!.on("data", (d: any) => { if (out.length < 1 << 20) out += d })
      child.on("error", () => { clearTimeout(timer); resolve(undefined) })
      child.on("close", () => {
        clearTimeout(timer)
        try { resolve(out.trim() ? JSON.parse(out) : undefined) } catch { resolve(undefined) }
      })
      child.stdin!.on("error", () => {})
      child.stdin!.end(JSON.stringify(input))
    } catch {
      resolve(undefined)
    }
  })
}

export default Plugin.define({
  id: "shikii",
  async setup(ctx) {
    const cwd = (): string | undefined => ctx.location?.directory
    const brief = new Map<string, string>() // sessionID -> the session-start text ("" when there was none); absent: ask at the next request
    const turn = new Map<string, string>() // sessionID -> the user-prompt-submit text of the person's last request
    const continued = new Map<string, boolean>() // sessionID -> the running turn was started by this plugin (stop_hook_active)
    const chain = new Map<string, number>() // sessionID -> continuations this plugin started since the person's last request
    const generation = new Map<string, number>() // sessionID -> counts the person's requests: a slow stop of an older one is dropped
    const running = new Set<string>() // sessionIDs with a turn this instance saw start and has not stopped yet
    const seen = new Set<string>() // sessionIDs this instance saw a request of
    const sessions = new Map<string, { child: boolean; dir?: string }>()

    // a session of this plugin's own project that is not a subagent's: Claude Code runs no UserPromptSubmit or Stop for a subagent's
    // session, and a session of another folder belongs to the plugin instance of that folder. When unsure, add nothing.
    const mine = async (sessionID: string): Promise<boolean> => {
      try {
        let s = sessions.get(sessionID)
        if (!s) {
          const got: any = await ctx.session.get({ sessionID } as any)
          s = { child: Boolean(got?.parentID), dir: got?.location?.directory }
          sessions.set(sessionID, s)
        }
        const here = cwd()
        return !s.child && (!here || !s.dir || s.dir === here)
      } catch {
        return false
      }
    }

    // the one way a hook is run: shikii gets the session and the folder, plus what the event adds
    const hook = (event: string, sessionID: string, extra: Record<string, unknown> = {}): Promise<any> =>
      run(event, { session_id: sessionID, cwd: cwd(), ...extra }, cwd())

    const additional = (out: any): string => {
      const text = out?.hookSpecificOutput?.additionalContext
      return typeof text === "string" ? text : ""
    }

    const turnNumber = (sessionID: string): number => generation.get(sessionID) ?? 0
    const bump = (sessionID: string) => generation.set(sessionID, turnNumber(sessionID) + 1)

    ctx.session.hook("prompt", async (e: any) => {
      try {
        const sessionID: string = e.sessionID
        if (!sessionID || !(await mine(sessionID))) return
        seen.add(sessionID)
        running.add(sessionID)
        if (e.metadata?.shikii === CONTINUE_KEY) {
          continued.set(sessionID, true) // the turn goes on after a stop block: nothing new from the person
          return
        }
        continued.set(sessionID, false)
        chain.set(sessionID, 0)
        bump(sessionID)
        if (!brief.has(sessionID)) brief.set(sessionID, additional(await hook("session-start", sessionID)))
        turn.set(sessionID, additional(await hook("user-prompt-submit", sessionID, { prompt: e.prompt?.text ?? "" })))
      } catch {
        // add nothing
      }
    })

    // text put into the system text is not kept in the conversation: every model call of the turn, a continuation's too, gets both
    ctx.session.hook("context", async (e: any) => {
      try {
        for (const text of [brief.get(e.sessionID), turn.get(e.sessionID)]) {
          if (text) e.system.push({ type: "text", text })
        }
      } catch {
        // add nothing
      }
    })

    // the end of a turn: the last assistant message goes to shikii hook stop; a block continues the turn with its reason
    const stop = async (sessionID: string) => {
      try {
        if (!sessionID || !running.delete(sessionID)) return // not a turn this instance saw start (a summary alone, another folder's session)
        const started = turnNumber(sessionID)
        if (!(await mine(sessionID))) return
        const messages: any = await ctx.session.context({ sessionID })
        const list: any[] = Array.isArray(messages) ? messages : []
        const last = [...list].reverse().find((m) => m?.type === "assistant")
        const reply = (last?.content ?? [])
          .filter((part: any) => part?.type === "text")
          .map((part: any) => part.text)
          .join("\n")
        const active = continued.get(sessionID) === true
        continued.set(sessionID, false) // read once: a continuation this stop sends sets it again when its prompt arrives
        const out = await hook("stop", sessionID, { last_assistant_message: reply, stop_hook_active: active })
        if (turnNumber(sessionID) !== started) return // the person's next request came while stop ran: its answer is not for that one
        const count = chain.get(sessionID) ?? 0
        if (out?.decision === "block" && typeof out.reason === "string" && out.reason && out.continue !== false && count < CONTINUE_MAX) {
          chain.set(sessionID, count + 1)
          await ctx.session.prompt({ sessionID, text: out.reason, metadata: { shikii: CONTINUE_KEY } } as any)
        }
      } catch {
        // add nothing
      }
    }

    // a subagent that finished puts its report into the parent session, and that wakes the parent for a turn the person did not ask for
    // (no prompt hook runs): like Claude Code's notification of a finished task, its stop is not stop_hook_active
    const woken = async (sessionID: string) => {
      if (!sessionID || running.has(sessionID) || !(await mine(sessionID)) || running.has(sessionID)) return
      running.add(sessionID)
      bump(sessionID)
    }

    const onEvent = async (ev: any) => {
      try {
        const sid: string | undefined = ev?.data?.sessionID
        if (!sid) return
        if (ev.type === "session.execution.succeeded") await stop(sid)
        else if (ev.type === "session.inbox.enqueued" && ev.data?.item?.type === "synthetic" && ev.data.item.payload?.metadata?.source === "subagent") await woken(sid)
        else if (ev.type === "session.compaction.ended" && seen.has(sid)) {
          // the summary replaced the past the brief was in: ask again now, the rest of the turn needs it; until it answers the old one stays
          const out = await hook("session-start", sid)
          if (out !== undefined) brief.set(sid, additional(out))
        }
      } catch {
        // add nothing
      }
    }

    const controller = new AbortController()
    void (async () => {
      try {
        // not awaited one by one: a slow stop must not hold back the events of other sessions
        for await (const ev of ctx.event.subscribe({ signal: controller.signal }) as any) void onEvent(ev)
      } catch {
        // the subscription ended
      }
    })()
    return () => controller.abort()
  },
})
