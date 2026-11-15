// shikii-managed OpenCode plugin. Written by `shikii hook install`; do not edit (a rerun rewrites it, `shikii hook uninstall` removes it).
//
// OpenCode 2.x has no JSON hook settings, so this plugin calls the same `shikii hook <event>` commands Claude Code and Codex call,
// with the same JSON on stdin:
//   - the person's request          -> shikii hook user-prompt-submit (its additionalContext goes into the model's system text)
//   - the first request of a session -> shikii hook session-start
//   - the end of a turn             -> shikii hook stop (a block makes OpenCode continue the turn with the reason)
// Every hook is wrapped in try/catch and a failure or a timeout adds nothing: a hook must never stop the work
// (a throw in the prompt or context hook would make OpenCode refuse the request).
import { Plugin } from "@opencode/plugin"
import { spawn } from "node:child_process"

// The absolute path of the shikii binary (replaced when shikii writes this file). It is run directly, not through a shell.
const SHIKII: string = "__SHIKII_BIN__"
const TIMEOUT_MS: Record<string, number> = { "user-prompt-submit": 15_000, "session-start": 15_000, stop: 20_000 }
const CONTINUE_KEY = "stop-continue" // metadata of a turn this plugin started: the next stop is stop_hook_active
const CONTINUE_MAX = 8 // a safety net: after this many continuations in a row for one request, the plugin stops continuing

// run starts `shikii hook <event>`, passes input as JSON on stdin and returns the JSON it prints (undefined when it prints none, fails or times out).
function run(event: string, input: Record<string, unknown>): Promise<any> {
  return new Promise((resolve) => {
    try {
      const child = spawn(SHIKII, ["hook", event], { stdio: ["pipe", "pipe", "ignore"] })
      let out = ""
      const timer = setTimeout(() => {
        try { child.kill() } catch {}
        resolve(undefined)
      }, TIMEOUT_MS[event] ?? 15_000)
      child.stdout.on("data", (d) => { if (out.length < 1 << 20) out += d })
      child.on("error", () => { clearTimeout(timer); resolve(undefined) })
      child.on("close", () => {
        clearTimeout(timer)
        try { resolve(out.trim() ? JSON.parse(out) : undefined) } catch { resolve(undefined) }
      })
      child.stdin.on("error", () => {})
      child.stdin.end(JSON.stringify(input))
    } catch {
      resolve(undefined)
    }
  })
}

export default Plugin.define({
  id: "shikii",
  async setup(ctx) {
    const cwd = (): string | undefined => ctx.location?.directory
    const added = new Map<string, string[]>() // sessionID -> text added to the system text of this turn's model calls
    const continued = new Map<string, boolean>() // sessionID -> the running turn was started by this plugin (stop_hook_active)
    const chain = new Map<string, number>() // sessionID -> continuations this plugin started since the person's last request
    const started = new Set<string>() // sessions that already had their session-start
    const children = new Map<string, boolean>() // sessionID -> a subagent session (the person's request is not in it)

    // a subagent's session has a parent: Claude Code does not run UserPromptSubmit or Stop for those, so neither does this plugin
    const isChild = async (sessionID: string): Promise<boolean> => {
      try {
        if (!children.has(sessionID)) {
          const s: any = await ctx.session.get({ sessionID } as any)
          children.set(sessionID, Boolean(s?.parentID))
        }
        return children.get(sessionID) === true
      } catch {
        return true // when unsure, add nothing
      }
    }

    ctx.session.hook("prompt", async (e: any) => {
      try {
        const sessionID: string = e.sessionID
        if (!sessionID || (await isChild(sessionID))) return
        if (e.metadata?.shikii === CONTINUE_KEY) {
          continued.set(sessionID, true) // the turn goes on after a stop block: nothing new from the person
          return
        }
        continued.set(sessionID, false)
        chain.set(sessionID, 0)
        const texts: string[] = []
        if (!started.has(sessionID)) {
          started.add(sessionID)
          const brief = await run("session-start", { session_id: sessionID, cwd: cwd() })
          const text = brief?.hookSpecificOutput?.additionalContext
          if (typeof text === "string" && text) texts.push(text)
        }
        const out = await run("user-prompt-submit", { session_id: sessionID, cwd: cwd(), prompt: e.prompt?.text ?? "" })
        const text = out?.hookSpecificOutput?.additionalContext
        if (typeof text === "string" && text) texts.push(text)
        added.set(sessionID, texts)
      } catch {
        // add nothing
      }
    })

    ctx.session.hook("context", async (e: any) => {
      try {
        for (const text of added.get(e.sessionID) ?? []) e.system.push({ type: "text", text })
      } catch {
        // add nothing
      }
    })

    // the end of a turn: the last assistant message goes to shikii hook stop; a block continues the turn with its reason
    const stop = async (sessionID: string) => {
      try {
        if (!sessionID || (await isChild(sessionID))) return
        const messages: any = await ctx.session.context({ sessionID })
        const list: any[] = Array.isArray(messages) ? messages : []
        const last = [...list].reverse().find((m) => m?.type === "assistant")
        const reply = (last?.content ?? [])
          .filter((part: any) => part?.type === "text")
          .map((part: any) => part.text)
          .join("\n")
        const active = continued.get(sessionID) === true
        const out = await run("stop", { session_id: sessionID, cwd: cwd(), last_assistant_message: reply, stop_hook_active: active })
        if (out?.decision === "block" && typeof out.reason === "string" && out.reason && out.continue !== false && (chain.get(sessionID) ?? 0) < CONTINUE_MAX) {
          chain.set(sessionID, (chain.get(sessionID) ?? 0) + 1)
          await ctx.session.prompt({ sessionID, text: out.reason, metadata: { shikii: CONTINUE_KEY } } as any)
        }
      } catch {
        // add nothing
      }
    }

    const controller = new AbortController()
    void (async () => {
      try {
        for await (const ev of ctx.event.subscribe({ signal: controller.signal }) as any) {
          try {
            if (ev?.type === "session.execution.succeeded") await stop(ev.data?.sessionID)
          } catch {
            // go on with the next event
          }
        }
      } catch {
        // the subscription ended
      }
    })()
    return () => controller.abort()
  },
})
