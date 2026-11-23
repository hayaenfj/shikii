package main

// Whether a session forget is asked to remove may still be running. The hook notes the session's agent process in its record at each
// event (agent_process.go), and that decides, whatever the time since the last record:
//
//   - the noted process is on this machine, with the same pid and the same start time: the agent is open. The session is running when
//     its last turn has not ended, a subagent it started is still open, or something it may have started still runs, and forget refuses;
//     otherwise it is idle, and forget goes on (forget_idle.go);
//   - the noted process is not there, or the pid belongs to a process that started later: the agent has ended, and forget goes on;
//   - no process can be compared (none was noted: a record of an older shikii, or the hook found no agent; or it was noted on another
//     machine; or this machine's process table could not be read): shikii cannot tell, so it goes by the time of the newest record of
//     the session and refuses until forgetUnknownQuiet has passed.
//
// A subagent's id is judged by the session it ran under.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// processNow compares the process a record names with the process table of this machine. When it cannot, the reason is given.
func processNow(p *agentProcess) (state processState, why string) {
	if p == nil {
		return processUnknown, "no agent process is noted for it: its records are from before the hook noted one, or the hook found no agent"
	}
	here, err := machineName()
	if err != nil || here == "" {
		return processUnknown, "the name of this machine is not known, so a noted process cannot be looked for"
	}
	if p.Host != here {
		return processUnknown, "its agent process was noted on another machine (" + p.Host + ")"
	}
	ctx, cancel := context.WithTimeout(context.Background(), hookProcessTimeout)
	defer cancel()
	row, state := readProcess(ctx, p.PID)
	switch {
	case state == processUnknown:
		return processUnknown, "the process table of this machine could not be read"
	case state == processFound && row.Start == p.Start:
		return processFound, ""
	case state == processAbsent && !processShowsItself(ctx):
		// a /proc that is empty or of another pid namespace says "not there" of every pid
		return processUnknown, "the process table of this machine does not show this very process, so it cannot say that another one has ended"
	}
	return processAbsent, "" // not there, or another process with the same pid
}

// processShowsItself says whether the process table shows the process that asks, with its start time: a table that does not cannot be
// believed when it says that another process is not there.
func processShowsItself(ctx context.Context) bool {
	row, state := readProcess(ctx, thisPID())
	return state == processFound && row.Start != ""
}

// processOfSession is the agent process the record of a session names, read from its file: the record of a session that is not one of
// the ids asked for (a subagent's id is judged by the session it ran under) was not read with the others.
func (r *forgetRun) processOfSession(session string) *agentProcess {
	data, err := r.st.ReadFileWhole(sessionPath(r.root, session))
	if err != nil {
		return nil
	}
	var doc map[string]any
	if json.Unmarshal(data, &doc) != nil {
		return nil
	}
	return agentProcessOf(doc)
}

// stillRunning says why the id may still be running, or "" when it has ended, is idle in an agent that is still open, or has been quiet
// for forgetUnknownQuiet with nothing to tell the process by.
func (r *forgetRun) stillRunning(id string) string {
	subject, session := id, id
	var p *agentProcess
	if parent, only := r.childOnly[id]; only {
		subject, session = id+" (a subagent that ran under the session "+parent+")", parent
		p = r.processOfSession(parent)
	} else {
		p = r.procs[id]
	}
	t := r.readTurn(session) // also of a session whose agent is gone: it may be resumed while forget runs
	state, why := processNow(p)
	switch state {
	case processFound:
		return r.aliveWhy(subject, p, t)
	case processAbsent:
		return ""
	}
	at, ok := r.newest[id]
	if !ok || time.Since(at) >= forgetUnknownQuiet {
		return ""
	}
	wait := (forgetUnknownQuiet - time.Since(at)).Round(time.Minute)
	return fmt.Sprintf("a record of %s was written %s ago, and shikii cannot tell whether the session is still running (%s); try again in about %s", id, time.Since(at).Round(time.Second), why, wait)
}
