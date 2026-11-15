// shikii-managed OpenCode plugin. Written by `shikii hook install`; do not edit (a rerun rewrites it, `shikii hook uninstall` removes it).
//
// OpenCode 2.x has no JSON hook settings, so this plugin calls the same `shikii hook <event>` commands Claude Code and Codex call,
// with the same JSON on stdin:
//   - the person's request          -> shikii hook user-prompt-submit (its additionalContext goes into the model's system text)
//   - the first request of a session, and right after a summary of the conversation (compaction) -> shikii hook session-start
//   - the end of a turn             -> shikii hook stop (a block makes OpenCode continue the turn with the reason)
// A turn is what this plugin saw start: the person's request, a continuation it sent itself, or a subagent's report that wakes the
// session. An execution that no turn of those started (a summary the person asked for) runs no stop, and neither does a turn that was
// interrupted or failed (Claude Code runs no Stop for those). One OpenCode server runs a plugin
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
    const sessions = new Map<string, { child: boolean; dir?: string }>() // what ctx.session.get said; forgotten when the session moves
    // the text of the assistant message a turn ended with, from session.text.ended: a summary in the middle of a turn takes the turn's
    // messages out of ctx.session.context, and stop would get no reply
    const texts = new Map<string, { id: string; byOrdinal: Map<number, string> }>()
    // Known limit: a turn is only in this instance's memory. When OpenCode reloads the plugins (a file under plugins/ changed) the turns
    // that were running get no stop. Keeping them in ctx.storage is not done: whether it is per project has not been checked.

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

    // ask shikii for the session's brief again and keep it; until it answers, the old one stays
    const refreshBrief = async (sessionID: string) => {
      const out = await hook("session-start", sessionID)
      if (out !== undefined) brief.set(sessionID, additional(out))
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
        // not asked again where the session is: running was only set for a session of this folder, and a session moved while its turn
        // ran still ends the turn here, in the folder that prepared the request (the instance of the new folder never saw it start)
        const messages: any = await ctx.session.context({ sessionID })
        const list: any[] = Array.isArray(messages) ? messages : []
        const last = [...list].reverse().find((m) => m?.type === "assistant")
        let reply = (last?.content ?? [])
          .filter((part: any) => part?.type === "text")
          .map((part: any) => part.text)
          .join("\n")
        const kept = texts.get(sessionID)
        texts.delete(sessionID)
        if (!reply && kept) reply = [...kept.byOrdinal.entries()].sort((a, b) => a[0] - b[0]).map((e) => e[1]).join("\n")
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
    // (no prompt hook runs): like Claude Code's notification of a finished task, its stop is not stop_hook_active. The event reaches the
    // plugin of every folder, so only a session this instance saw a request of is woken.
    const woken = async (sessionID: string) => {
      if (!sessionID || !seen.has(sessionID) || running.has(sessionID) || !(await mine(sessionID)) || running.has(sessionID)) return
      running.add(sessionID)
      continued.set(sessionID, false) // the number of the person's requests is not moved: a stop that runs now still has its answer heard
    }

    // a turn that ended without succeeding gets no stop (Claude Code runs no Stop when the person interrupts a turn or the model fails),
    // so it is no longer running: a summary asked for afterwards runs no stop. A turn superseded by the person's next message is left
    // alone, the prompt hook of that message may already have marked the new turn.
    const ended = (sessionID: string) => {
      running.delete(sessionID) // continued is read at the next stop only: the next request and a woken turn set it again
      texts.delete(sessionID) // the reply of a turn that did not succeed is no turn's reply
    }

    const onEvent = async (ev: any) => {
      try {
        const sid: string | undefined = ev?.data?.sessionID
        if (!sid) return
        if (ev.type === "session.execution.succeeded") await stop(sid)
        else if (ev.type === "session.execution.failed" || (ev.type === "session.execution.interrupted" && ev.data?.reason !== "superseded")) ended(sid)
        else if (ev.type === "session.inbox.enqueued" && ev.data?.item?.type === "synthetic" && ev.data.item.payload?.metadata?.source === "subagent") await woken(sid)
        else if (ev.type === "session.moved") {
          sessions.delete(sid) // ask again where it is now
          if (await mine(sid)) {
            // it came to this folder: take it over. The rest of a turn that was running goes on under this instance's context hook, so
            // it gets this project's brief now (the request's own text cannot be handed over: the request was counted where it was made)
            seen.add(sid)
            await refreshBrief(sid)
          }
        }
        else if (ev.type === "session.text.ended" && running.has(sid) && typeof ev.data?.text === "string") {
          const id = String(ev.data.assistantMessageID)
          let t = texts.get(sid)
          if (!t || t.id !== id) texts.set(sid, (t = { id, byOrdinal: new Map() }))
          t.byOrdinal.set(Number(ev.data.ordinal) || 0, ev.data.text)
        } else if (ev.type === "session.compaction.ended" && seen.has(sid) && (await mine(sid))) {
          await refreshBrief(sid) // the summary replaced the past the brief was in: the rest of the turn needs it
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
