// shikii-managed OpenCode plugin. Written by `shikii hook install`; do not edit (a rerun rewrites it, `shikii hook uninstall` removes it).
//
// OpenCode 2.x has no JSON hook settings, so this plugin calls the same `shikii hook <event>` commands Claude Code and Codex call,
// with the same JSON on stdin:
//   - the person's request          -> shikii hook user-prompt-submit (its additionalContext goes on the request's message). turn_key is the
//     id of that message, and the stop that ends the turn names the same one: a stop that reaches shikii after the next request was
//     noted is not taken for the end of the new turn
//   - the first request of a session, and right after a summary of the conversation (compaction) -> shikii hook session-start
//   - the end of a turn             -> shikii hook stop (a block makes OpenCode continue the turn with the reason). It runs in OpenCode's
//     experimental.idle hook, inside the execution: opencode run exits as soon as the execution ends, and ends the private server it
//     started (--standalone, or no background service running) with it, before a stop started from session.execution.succeeded can run
//   - a subagent's task             -> shikii hook subagent-start, with the parent's session, the subagent's own session as agent_id and
//     the task as prompt (its additionalContext goes on the task's message)
//   - a subagent going idle         -> shikii hook subagent-stop, with its last message: shikii reads what the subagent says it did with
//     the lessons it was given. Its answer is not read and the subagent is never continued. The end of a subagent is told also when there
//     is no new message to read (or the turn failed), and when its report wakes the parent: shikii keeps a subagent open until then
// A turn is what this plugin saw start: the person's request, a continuation it sent itself, or a subagent's report that wakes the
// session. An execution that no turn of those started (a summary the person asked for) runs no stop, and neither does a turn that was
// interrupted or failed (Claude Code runs no Stop for those); a turn the person interrupts while its stop runs is not continued. One
// OpenCode server runs a plugin
// instance per project folder, and every instance is told every session's events: an instance acts only on sessions of its own folder.
// What the hooks give is not kept in the conversation, so the plugin adds it to every model call again, at the same place: each
// request's text on that request's own message (as Claude Code keeps it in the conversation), and the session's brief (the text of
// session-start) on the first request that comes after the plugin asked shikii for it. The plugin numbers the person's requests as
// they come, and keeps with each ask the number it was at (the request's own, or the next request's for an ask with no request: after
// a move or a summary). So where the brief goes is worked out from numbers, not from the order the answers come back in: an answer
// that comes late does not take the brief away from a request that came before it; of two asks of one time the smaller number (the
// request that asked first) wins; a newer ask (a move, a summary) replaces an older one. In a new conversation that is its first
// request; in a session resumed in a new process, the first request that process saw, not the conversation's first message, where the
// model would read a due memory nap as an old text of a request long past; after a move, the next request. Until that request is among
// the model's messages the brief is on the newest of the person's requests the model has (fixed at the first call that needs it, and
// moved once, to the request, when it comes; a message that is no request, a tool result that has the user role, is never chosen).
// A summary of the conversation is itself the first message after the text came out: the brief asked for again then stays on the
// summary. The system text is left as it is: a provider caches the start of a request, and a system text that changed with every
// request made it start over each time. Every hook is wrapped in try/catch and a failure or a timeout adds nothing: a hook must never
// stop the work (a throw in the prompt or context hook would make OpenCode refuse the request).
import { Plugin } from "@opencode/plugin"
import { spawn } from "node:child_process"

// The absolute path of the shikii binary (replaced when shikii writes this file). It is run directly, not through a shell.
const SHIKII: string = "__SHIKII_BIN__"
const TIMEOUT_MS: Record<string, number> = { "user-prompt-submit": 15_000, "session-start": 15_000, stop: 20_000 }
const CONTINUE_KEY = "stop-continue" // metadata of a turn this plugin started: the next stop is stop_hook_active
const CONTINUE_MAX = 8 // a safety net: after this many continuations in a row for one request, the plugin stops continuing
// measured on 2.3.0: OpenCode puts this line before the task the parent gave a subagent. The task is passed without it (since 0.37.2
// shikii no longer picks lessons by the task: a subagent starts with the rules for every request)
const SUBAGENT_PREFACE = "You are a subagent spawned by another session.\n"
const REQUESTS_KEPT = 64 // the person's last requests kept per session (the number and the message id of each)

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
    // sessionID -> the session-start text, asked for at the first request this plugin instance saw of the session (the session's first, or
    // in a session resumed in a new process that process's first). "" when that gave nothing (none, a failure, a timeout): it is not
    // asked for again until a summary or a move asks again, as Claude Code runs SessionStart only at those times
    const brief = new Map<string, string>()
    // sessionID -> the person's requests this instance saw: how many (the last number given; a request is numbered as its prompt hook
    // runs, so the numbers are the order the requests came in) and the last REQUESTS_KEPT of them with their message ids ("" when OpenCode
    // gave none)
    const requests = new Map<string, { count: number; list: { id: string; n: number }[] }>()
    // sessionID -> how many times the brief was asked for again with no request of the person's (a move, a summary): a newer ask
    // (generation) replaces an older one. A request's own ask is of the generation there is when it comes.
    const askGen = new Map<string, number>()
    // sessionID -> the number of the first request this plugin instance numbered since the session came to this folder: a move asks
    // again at that number, so a request sent here before session.moved reached the plugin is where the brief of the move goes
    const hereFrom = new Map<string, number>()
    // sessionID -> where the brief goes. gen, ask: the generation and the number the answered ask was made at (an ask with no request is
    // made at the number the next request will get). summary: asked after a summary, so the brief stays on the summary. target: the
    // message id of the request the brief belongs on, the first one numbered ask or more (found once and kept; none until one comes; ""
    // when OpenCode gave none: the brief goes into the system text, as a request's text does then). seen: that message was in a model
    // call. fixed: where the brief is until the request has reached the model: the newest of the person's requests the model had then.
    const briefOn = new Map<string, { gen: number; ask: number; summary: boolean; target?: string; seen: boolean; fixed?: string }>()
    // sessionID -> the person's requests: messageID -> the user-prompt-submit text, and whether that message was in a model call yet.
    // "" is the messageID when OpenCode gave none. current is the person's last request.
    const notes = new Map<string, Map<string, { text: string; seen: boolean }>>()
    const current = new Map<string, string>()
    const continued = new Map<string, boolean>() // sessionID -> the running turn was started by this plugin (stop_hook_active)
    const chain = new Map<string, number>() // sessionID -> continuations this plugin started since the person's last request
    // sessionID -> counts the person's requests and the turns that ended without succeeding: a stop that was running when the number
    // moved drops its answer (it is for a turn that is over)
    const generation = new Map<string, number>()
    const running = new Set<string>() // sessionIDs with a turn this instance saw start and has not stopped yet
    const wakes = new Map<string, Promise<unknown>>() // sessionID -> the hook that told shikii a subagent's report woke the session
    const seen = new Set<string>() // sessionIDs this instance saw a request of
    // what ctx.session.get said (parent: the session a subagent's session was started from; busy: a turn runs); forgotten when the session moves
    const sessions = new Map<string, { child: boolean; parent?: string; agent?: string; dir?: string; busy: boolean }>()
    const briefed = new Set<string>() // subagents' sessionIDs whose task went to shikii hook subagent-start
    const working = new Set<string>() // subagents' sessionIDs with a turn this instance saw start: its end goes to shikii hook subagent-stop once
    const reported = new Map<string, string>() // subagent's sessionID -> its last message that went to shikii hook subagent-stop
    const ends = new Map<string, number>() // sessionID -> executions seen ending (any folder's)
    const inherited = new Set<string>() // sessions moved here while a turn of the old folder ran: that turn ends there, a report joining it wakes nothing here
    // the text of the assistant message a turn ended with, from session.text.ended: a summary in the middle of a turn takes the turn's
    // messages out of ctx.session.context, and stop would get no reply
    const texts = new Map<string, { id: string; byOrdinal: Map<number, string> }>()
    // Everything above is per session and forgotten when the session is deleted (session.deleted, sent to every instance).
    // Known limit: a turn is only in this instance's memory. When OpenCode reloads the plugins (a file under plugins/ changed) the turns
    // that were running get no stop. Keeping them in ctx.storage is not done: whether it is per project has not been checked.

    // what OpenCode says of a session, asked once. undefined when it cannot be asked, or when the session is another folder's: that
    // one belongs to the plugin instance of that folder.
    const here = async (sessionID: string) => {
      try {
        let s = sessions.get(sessionID)
        if (!s) {
          const got: any = await ctx.session.get({ sessionID } as any)
          s = {
            child: Boolean(got?.parentID),
            parent: typeof got?.parentID === "string" ? got.parentID : undefined,
            agent: typeof got?.agent === "string" ? got.agent : undefined,
            dir: got?.location?.directory,
            busy: (got?.time?.busy ?? 0) > (got?.time?.idle ?? 0),
          }
          sessions.set(sessionID, s)
        }
        const dir = cwd()
        return !dir || !s.dir || s.dir === dir ? s : undefined
      } catch {
        return undefined
      }
    }

    // a session of this plugin's own project that is not a subagent's: Claude Code runs no UserPromptSubmit or Stop for a subagent's
    // session. When unsure, add nothing.
    const mine = async (sessionID: string): Promise<boolean> => {
      const s = await here(sessionID)
      return s !== undefined && !s.child
    }

    // a subagent's session of this plugin's own project: the session it was started from and the agent it runs as
    const subagentOf = async (sessionID: string): Promise<{ parent: string; agent: string } | undefined> => {
      const s = await here(sessionID)
      return s?.child && s.parent ? { parent: s.parent, agent: s.agent ?? "" } : undefined
    }

    // the one way a hook is run: shikii gets the session and the folder, plus what the event adds
    const hook = (event: string, sessionID: string, extra: Record<string, unknown> = {}): Promise<any> =>
      run(event, { session_id: sessionID, cwd: cwd(), ...extra }, cwd())

    const additional = (out: any): string => {
      const text = out?.hookSpecificOutput?.additionalContext
      return typeof text === "string" ? text : ""
    }

    // a request of the person's is numbered as it comes, and its message id is kept; the number is returned
    const noteRequest = (sessionID: string, id: string): number => {
      let r = requests.get(sessionID)
      if (!r) requests.set(sessionID, (r = { count: 0, list: [] }))
      r.list.push({ id, n: ++r.count })
      if (r.list.length > REQUESTS_KEPT) r.list.shift()
      return r.count
    }

    // the request the brief belongs on is the first one numbered at or after the number it was asked for at; once there is one it is kept
    // (a long session drops its old requests from the list)
    const resolveBrief = (sessionID: string) => {
      const on = briefOn.get(sessionID)
      if (!on || on.summary || on.target !== undefined) return
      const found = requests.get(sessionID)?.list.find((q) => q.n >= on.ask)
      if (found) on.target = found.id
    }

    // an answer of session-start. Where it goes is worked out from the numbers, not from the order the answers come in: an answer of an
    // older generation than the one placed is of the past and is dropped, a newer generation replaces the place, and of one generation
    // the smaller number wins (the request that asked first, also when its answer comes last; for one number the ask after a summary).
    const answerBrief = (sessionID: string, gen: number, ask: number, summary: boolean, text: string) => {
      const on = briefOn.get(sessionID)
      if (on && on.gen > gen) return
      brief.set(sessionID, text)
      if (!on || on.gen < gen || ask < on.ask || (ask === on.ask && summary && !on.summary)) {
        briefOn.set(sessionID, { gen, ask, summary, seen: false })
        resolveBrief(sessionID)
      }
    }

    // ask shikii for the session's brief again with no request of the person's; until it answers, the old one stays where it was. After a
    // summary it goes on the summary. After a move it goes on the first request that comes after the ask, also one that came before the
    // answer.
    const refreshBrief = async (sessionID: string, summary: boolean) => {
      const gen = (askGen.get(sessionID) ?? 0) + 1
      askGen.set(sessionID, gen)
      const ask = (!summary && hereFrom.get(sessionID)) || (requests.get(sessionID)?.count ?? 0) + 1
      const out = await hook("session-start", sessionID)
      if (out === undefined) return
      answerBrief(sessionID, gen, ask, summary, additional(out))
    }

    const turnNumber = (sessionID: string): number => generation.get(sessionID) ?? 0
    const bump = (sessionID: string) => generation.set(sessionID, turnNumber(sessionID) + 1)

    // The start of a subagent goes to shikii hook subagent-start under the parent's session (as Claude Code's SubagentStart does), with
    // its task, and what shikii gives (the rules for every request and how to report) goes on the task's message. Only the first message of a subagent's session is its task; a later one (the
    // parent continuing it) starts another turn of the same subagent, whose end is reported again, so its start is told again too (shikii
    // keeps a subagent open from its start to its end, and nothing else tells the parent's session that the subagent thinks again): that
    // start carries no task and says it is a later turn (continued), so shikii keeps the subagent open and delivers and records nothing, as
    // the subagent has what it was given. A message that joins a turn that is running is no new turn.
    const subagentPrompt = async (sessionID: string, sub: { parent: string; agent: string }, e: any) => {
      const began = !working.has(sessionID)
      working.add(sessionID)
      if (briefed.has(sessionID)) {
        if (began) await hook("subagent-start", sub.parent, { agent_id: sessionID, agent_type: sub.agent, continued: true })
        return
      }
      briefed.add(sessionID)
      const given: string = e.prompt?.text ?? ""
      const task = given.startsWith(SUBAGENT_PREFACE) ? given.slice(SUBAGENT_PREFACE.length) : given
      const out = await hook("subagent-start", sub.parent, { agent_id: sessionID, agent_type: sub.agent, prompt: task })
      const id = typeof e.messageID === "string" ? e.messageID : ""
      current.set(sessionID, id)
      const text = additional(out)
      if (text) notes.set(sessionID, new Map([[id, { text, seen: false }]]))
    }

    // the end of a subagent's turn: its last message goes to shikii hook subagent-stop, which answers nothing. The subagent is never
    // continued, so the report its parent gets is the one it wrote.
    const subagentStop = async (sessionID: string) => {
      try {
        if (!sessionID || !working.delete(sessionID)) return
        const sub = await subagentOf(sessionID)
        if (!sub) return
        const messages: any = await ctx.session.context({ sessionID })
        const list: any[] = Array.isArray(messages) ? messages : []
        const last = [...list].reverse().find((m) => m?.type === "assistant")
        const reply = (last?.content ?? [])
          .filter((part: any) => part?.type === "text")
          .map((part: any) => part.text)
          .join("\n")
        // a continued subagent that ends a turn without a new message has nothing to report: its last message is the one already sent
        // (told apart by the message's id, so the same words in a new message are reported: measured on OpenCode 2.3.0, every
        // assistant message here carries one, "msg_…". Were there none, the words would tell, and a summary that drops earlier
        // messages changes nothing)
        const said = String(last?.id ?? "") + "\n" + reply
        // the end is told every time (shikii keeps a subagent open until then); the report only when there is a new one
        const fresh = Boolean(reply) && reported.get(sessionID) !== said
        if (fresh) reported.set(sessionID, said)
        await hook("subagent-stop", sub.parent, { agent_id: sessionID, agent_type: sub.agent, ...(fresh ? { last_assistant_message: reply } : {}) })
      } catch {
        // add nothing
      }
    }

    // a subagent's turn that did not succeed: shikii is told it ended, with nothing to report
    const subagentEnded = async (sessionID: string) => {
      try {
        const sub = await subagentOf(sessionID)
        if (sub) await hook("subagent-stop", sub.parent, { agent_id: sessionID, agent_type: sub.agent })
      } catch {
        // add nothing
      }
    }

    ctx.session.hook("prompt", async (e: any) => {
      try {
        const sessionID: string = e.sessionID
        if (!sessionID) return
        const sub = await subagentOf(sessionID)
        if (sub) return await subagentPrompt(sessionID, sub, e)
        if (!(await mine(sessionID))) return
        seen.add(sessionID)
        // a message the person sends while a turn that moved here still runs joins that execution: the old folder runs its one stop
        if (!inherited.has(sessionID)) running.add(sessionID)
        if (e.metadata?.shikii === CONTINUE_KEY) {
          continued.set(sessionID, true) // the turn goes on after a stop block: nothing new from the person
          return
        }
        continued.set(sessionID, false)
        chain.set(sessionID, 0)
        bump(sessionID)
        const id = typeof e.messageID === "string" ? e.messageID : ""
        // numbered before anything is waited for: the number is the order the requests came in. A brief asked for again with no request
        // (a move) goes on the first request numbered after the ask, and this may be it.
        const n = noteRequest(sessionID, id)
        if (!hereFrom.has(sessionID)) hereFrom.set(sessionID, n)
        resolveBrief(sessionID)
        const gen = askGen.get(sessionID) ?? 0
        // both at once: one after the other would add session-start's time to the first request (shikii's writes for the two are the
        // same and safe to run side by side, as they run for two conversations of one project)
        const first = brief.has(sessionID) ? undefined : hook("session-start", sessionID)
        const [started, prepared] = await Promise.all([first, hook("user-prompt-submit", sessionID, { prompt: e.prompt?.text ?? "", turn_key: typeof e.messageID === "string" ? e.messageID : "" })])
        if (first) answerBrief(sessionID, gen, n, false, additional(started)) // asked at this request's number
        current.set(sessionID, id)
        const text = additional(prepared)
        if (text) {
          let mine = notes.get(sessionID)
          if (!mine) notes.set(sessionID, (mine = new Map()))
          mine.set(id, { text, seen: false })
        }
      } catch {
        // add nothing
      }
    })

    // every model call gets the brief on the first request that came after it was asked for and each request's text on that request's
    // message, the same way each time
    ctx.session.hook("context", async (e: any) => {
      try {
        const sessionID: string = e.sessionID
        const list: any[] = Array.isArray(e.messages) ? e.messages : []
        const users = list.flatMap((m: any, i: number) => (m?.role === "user" ? [i] : []))
        const add = new Map<number, string[]>() // index in the messages -> texts to put after its own
        const loose: string[] = [] // texts with no message to go on: into the system text, rather than nowhere
        const put = (i: number | undefined, text: string | undefined) => {
          if (!text) return
          if (i === undefined) loose.push(text)
          else add.set(i, [...(add.get(i) ?? []), text])
        }
        const byId = new Map(users.map((i) => [String(list[i]?.id), i]))
        resolveBrief(sessionID)
        const on = briefOn.get(sessionID)
        const text = brief.get(sessionID)
        if (!on || on.summary || on.target === undefined) put(users[0], text) // after a summary: the summary. No request since it was asked for: the first message the model gets
        else if (!on.target) put(undefined, text) // OpenCode gave no messageID
        else if (byId.has(on.target)) {
          on.seen = true
          put(byId.get(on.target), text)
        } else if (on.seen) put(users[0], text) // a summary took the message: the rest of the session still needs the brief
        else {
          // that request is not among the model's messages yet (it waits for a step boundary, or was dropped): the newest of the person's
          // requests the model has, chosen once and kept; a user message that came with no request (a tool result) is not one
          let at = on.fixed === undefined ? undefined : byId.get(on.fixed)
          if (at === undefined) {
            const ids = new Set((requests.get(sessionID)?.list ?? []).map((q) => q.id).filter((id) => id))
            for (const [id, i] of byId) if (ids.has(id) && (at === undefined || i > at)) { at = i; on.fixed = id }
          }
          put(at ?? users[0], text)
        }
        const mine = notes.get(sessionID)
        if (mine) {
          for (const [id, note] of mine) {
            if (byId.has(id)) note.seen = true
            else if (!id) put(undefined, note.text) // OpenCode gave no messageID
            else if (!note.seen) continue // not in the model's messages yet (a message sent while a turn runs waits): it goes on it when it comes
            else if (id === current.get(sessionID)) put(users[0], note.text) // a summary took the request's message: the rest of its turn still needs it
            else mine.delete(id) // a summary took an older request's message, and its text with it
          }
          for (const [id, i] of byId) put(i, mine.get(id)?.text)
        }
        for (const [i, texts] of add) {
          const m = list[i]
          const own = typeof m.content === "string" ? [{ type: "text", text: m.content }] : Array.isArray(m.content) ? m.content : undefined
          // a copy: the message itself is left as it is. Parts are joined without a break by some providers, so each starts with one
          if (own) list[i] = { ...m, content: [...own, ...texts.map((text) => ({ type: "text", text: "\n\n" + text }))] }
          else loose.push(...texts)
        }
        for (const text of loose) e.system.push({ type: "text", text })
      } catch {
        // add nothing
      }
    })

    // the end of a turn: the last assistant message goes to shikii hook stop; a block continues the turn with its reason
    const stop = async (sessionID: string) => {
      try {
        if (!sessionID || !running.delete(sessionID)) return // not a turn this instance saw start (a summary alone, another folder's session)
        const started = turnNumber(sessionID)
        const key = current.get(sessionID) ?? "" // the message of the request this stop is of, read before anything is waited for
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
        await wakes.get(sessionID) // the hook that told shikii a report woke the turn comes before the stop that ends that turn
        // the person's next request came while the conversation was read: this stop is of a turn that is over, and shikii would take it for
        // the end of the new one
        if (turnNumber(sessionID) !== started) return
        const active = continued.get(sessionID) === true
        continued.set(sessionID, false) // read once: a continuation this stop sends sets it again when its prompt arrives
        const out = await hook("stop", sessionID, { last_assistant_message: reply, stop_hook_active: active, turn_key: key })
        // the person's next request came while stop ran, or the person interrupted the execution waiting on stop in the idle hook (OpenCode
        // stops waiting for the hook and ends the execution as interrupted): its answer does not continue a turn that is over
        if (turnNumber(sessionID) !== started) return
        const count = chain.get(sessionID) ?? 0
        if (out?.decision === "block" && typeof out.reason === "string" && out.reason && out.continue !== false && count < CONTINUE_MAX) {
          chain.set(sessionID, count + 1)
          await ctx.session.prompt({ sessionID, text: out.reason, metadata: { shikii: CONTINUE_KEY } } as any)
        }
      } catch {
        // add nothing
      }
    }

    // OpenCode 2.3.0 runs this hook when a turn has nothing left to do and waits for it before the execution ends: a continuation sent
    // here goes on in the same execution, so the execution (and opencode run with it) ends only after the last stop, as a Claude Code turn
    // ends after its Stop hook. A turn whose stop ran here runs none at session.execution.succeeded (running no longer has it); an OpenCode
    // that does not run this hook gets the stop at that event.
    try {
      const added: any = ctx.session.hook("experimental.idle", async (e: any) => {
        try {
          await subagentStop(e?.sessionID)
          await stop(e?.sessionID)
        } catch {
          // add nothing
        }
      })
      added?.catch?.(() => {})
    } catch {
      // an OpenCode that does not take the hook: stop runs at session.execution.succeeded
    }

    // a subagent that finished puts its report into the parent session, and that wakes the parent for a turn the person did not ask for
    // (no prompt hook runs): like Claude Code's notification of a finished task, its stop is not stop_hook_active. The event reaches the
    // plugin of every folder, so only a session this instance saw a request of is woken.
    const woken = async (sessionID: string, report: any) => {
      if (!sessionID || inherited.has(sessionID) || !seen.has(sessionID) || running.has(sessionID) || !(await mine(sessionID)) || running.has(sessionID)) return
      running.add(sessionID)
      continued.set(sessionID, false) // the number of the person's requests is not moved: a stop that runs now still has its answer heard
      // no hook runs for this turn, so shikii is told what woke it (the end of that subagent, with no report to read): the session works
      // again after the stop that came before, and it is not idle until the stop of this turn
      const child = typeof report?.childID === "string" ? report.childID : undefined
      const told = hook("subagent-stop", sessionID, { agent_id: child, agent_type: typeof report?.agent === "string" ? report.agent : undefined })
      wakes.set(sessionID, told)
      await told
    }

    // a turn that ended without succeeding gets no stop (Claude Code runs no Stop when the person interrupts a turn or the model fails),
    // so it is no longer running: a summary asked for afterwards runs no stop. A turn superseded by the person's next message is left
    // alone, the prompt hook of that message may already have marked the new turn.
    const ended = (sessionID: string) => {
      running.delete(sessionID) // continued is read at the next stop only: the next request and a woken turn set it again
      if (working.delete(sessionID)) void subagentEnded(sessionID) // a subagent's turn that did not succeed reports nothing, but it has ended
      texts.delete(sessionID) // the reply of a turn that did not succeed is no turn's reply
      bump(sessionID) // a stop still running in the idle hook does not continue the turn the person stopped
    }

    // a deleted session is gone for good: nothing of it is kept, so the tables do not grow with every session the server ever had
    const forget = (sessionID: string) => {
      for (const table of [brief, briefOn, requests, askGen, hereFrom, notes, current, continued, chain, generation, sessions, ends, texts, wakes]) table.delete(sessionID)
      for (const set of [running, seen, inherited, briefed, working, reported]) set.delete(sessionID)
    }

    const onEvent = async (ev: any) => {
      try {
        const sid: string | undefined = ev?.data?.sessionID
        if (!sid) return
        if (ev.type === "session.deleted") return forget(sid)
        if (ev.type === "session.execution.succeeded" || ev.type === "session.execution.failed" || ev.type === "session.execution.interrupted") {
          ends.set(sid, (ends.get(sid) ?? 0) + 1)
          inherited.delete(sid) // the execution that was the old folder's turn is over
        }
        if (ev.type === "session.execution.succeeded") {
          await subagentStop(sid) // a turn whose end the idle hook did not see (both are for one session at most: working or running has it)
          await stop(sid)
        }
        else if (ev.type === "session.execution.failed" || (ev.type === "session.execution.interrupted" && ev.data?.reason !== "superseded")) ended(sid)
        else if (ev.type === "session.inbox.enqueued" && ev.data?.item?.type === "synthetic" && ev.data.item.payload?.metadata?.source === "subagent") await woken(sid, ev.data.item.payload?.metadata)
        else if (ev.type === "session.moved") {
          const endsBefore = ends.get(sid) ?? 0
          sessions.delete(sid) // ask again where it is now
          if (!(await mine(sid))) hereFrom.delete(sid)
          else {
            // a turn that runs as it comes is the old folder's: its stop runs there, and a report that joins it wakes nothing here
            // (unless that execution ended while this asked)
            if (sessions.get(sid)?.busy && (ends.get(sid) ?? 0) === endsBefore) inherited.add(sid)
            // it came to this folder: take it over. The rest of a turn that was running goes on under this instance's context hook, so
            // it gets this project's brief now (the request's own text cannot be handed over: the request was counted where it was made)
            seen.add(sid)
            await refreshBrief(sid, false)
          }
        }
        else if (ev.type === "session.text.ended" && running.has(sid) && typeof ev.data?.text === "string") {
          const id = String(ev.data.assistantMessageID)
          let t = texts.get(sid)
          if (!t || t.id !== id) texts.set(sid, (t = { id, byOrdinal: new Map() }))
          t.byOrdinal.set(Number(ev.data.ordinal) || 0, ev.data.text)
        } else if (ev.type === "session.compaction.ended" && seen.has(sid) && (await mine(sid))) {
          // a request's message the summary took before any model call had it is gone as well: not waited for any more
          for (const note of notes.get(sid)?.values() ?? []) note.seen = true
          const on = briefOn.get(sid)
          if (on) on.seen = true // the same for the message that asked for the brief: if the summary took it, the old brief goes on the summary
          await refreshBrief(sid, true) // the summary replaced the past the brief was in: the rest of the turn needs it
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
