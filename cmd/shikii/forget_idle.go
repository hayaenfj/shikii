package main

// A session whose agent process is alive is not always at work: the agent stays open after a request ends. forget takes such a session
// when both of these are so, and refuses otherwise:
//
//   - it is not in the middle of a turn: the state of the session's turn (sessionLatest.InTurn) is opened by any message of the person
//     that reached the hook, a slash command that is no request among them, and closed by a Stop that lets the turn end. A person who
//     interrupts a turn may leave no Stop, and a command of the agent's app that calls the hook (/clear) may leave none; that
//     session is refused until its agent is closed.
//   - nothing the session may have started is still running: no process under the agent that started after the hook first saw the agent
//     run the session (Noted). Processes of the agent that started before are its own (the servers it starts with itself); forget
//     itself, what it started and everything above it are not counted either.
//
// What cannot be read (no state of the turn, no time the agent was noted at, a process list that cannot be read) is not idle: forget
// refuses and says why.

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"
)

// aliveWhy says why a session whose agent process p is alive is still at work, or "" when it is idle. subject names the id as the
// refusal says it, and session is the id whose state of the turn counts (a subagent's id is judged by the session it ran under).
func (r *forgetRun) aliveWhy(subject, session string, p *agentProcess) string {
	agent := fmt.Sprintf("its agent process (%s, pid %d) is alive", p.Name, p.PID)
	latest, known, err := readSessionLatest(session)
	switch {
	case err != nil:
		return r.cannotTell(subject, agent, "the state of its last turn could not be read: "+err.Error())
	case !known:
		return r.cannotTell(subject, agent, "no state of its last turn is kept: it is from an older shikii")
	case latest.InTurn == nil:
		return r.cannotTell(subject, agent, "its last turn was kept by an older shikii, which did not note whether it ended")
	case *latest.InTurn:
		return fmt.Sprintf("%s is still running: %s and its last turn has not ended (a request or a command began it, and no Stop that ends it was recorded); wait for the agent to finish it, or end that agent, then try again", subject, agent)
	case p.Noted.IsZero():
		return r.cannotTell(subject, agent, "its record does not say when the agent was first seen running it: it is from an older shikii")
	}
	started, err := r.startedUnder(p)
	if err != nil {
		return r.cannotTell(subject, agent, "the list of processes could not be read: "+err.Error())
	}
	if started == nil {
		return ""
	}
	return fmt.Sprintf("%s is still running: %s and a process under it that started after the session did is running (%s, pid %d, started %s); wait for it to end, or end that agent, then try again",
		subject, agent, processName(started.Name), started.PID, started.Started.UTC().Format(time.RFC3339))
}

func (r *forgetRun) cannotTell(subject, agent, why string) string {
	return fmt.Sprintf("%s may still be running: %s, and shikii cannot tell whether it is at work (%s); end that agent, then try again", subject, agent, why)
}

// processList is the list of this machine's processes, read once for each pass over the evidence (gather clears it).
func (r *forgetRun) processList() ([]processEntry, error) {
	if !r.listRead {
		ctx, cancel := context.WithTimeout(context.Background(), hookProcessTimeout)
		defer cancel()
		r.list, r.listErr = readProcessList(ctx)
		r.listRead = true
	}
	return r.list, r.listErr
}

// thisPID is the pid of the process that runs forget (a test gives another).
var thisPID = os.Getpid

// startedUnder is the oldest process under the agent that started after the agent was noted and is still running, or nil. Under means
// anywhere below it, through the parents the list names. Not counted: this process and everything it started (the ps it asked), and the
// processes above it (an agent that runs forget, in a shell, and an MCP server that runs it).
func (r *forgetRun) startedUnder(p *agentProcess) (*processEntry, error) {
	list, err := r.processList()
	if err != nil {
		return nil, err
	}
	byPID := make(map[int]processEntry, len(list))
	below := map[int][]int{}
	for _, e := range list {
		byPID[e.PID] = e
		below[e.Parent] = append(below[e.Parent], e.PID)
	}
	// a list that does not have this process is of another place (an empty /proc, another pid namespace): what stands above forget cannot
	// be told from what stands below the agent
	if self, ok := byPID[thisPID()]; !ok || self.Started.IsZero() {
		return nil, fmt.Errorf("the list does not show this very process (pid %d)", thisPID())
	}
	skip := map[int]bool{}
	for pid, hops := thisPID(), 0; pid > 1 && !skip[pid] && hops <= len(list); hops++ {
		skip[pid] = true
		e, ok := byPID[pid]
		if !ok {
			break
		}
		pid = e.Parent
	}
	for stack := []int{thisPID()}; len(stack) > 0; {
		pid := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, child := range below[pid] {
			if !skip[child] {
				skip[child] = true
				stack = append(stack, child)
			}
		}
	}
	// ps gives a start time to the second: a process that started in the second the agent was noted in is shown as started before it, and
	// is not counted. The window is under a second, and the first event of a session comes before anything the session starts.
	since := p.Noted
	var found []processEntry
	seen := map[int]bool{p.PID: true}
	for stack := append([]int(nil), below[p.PID]...); len(stack) > 0; {
		pid := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[pid] {
			continue
		}
		seen[pid] = true
		stack = append(stack, below[pid]...)
		if e := byPID[pid]; !skip[pid] && e.Started.After(since) {
			found = append(found, e)
		}
	}
	if len(found) == 0 {
		return nil, nil
	}
	sort.Slice(found, func(i, j int) bool {
		if !found[i].Started.Equal(found[j].Started) {
			return found[i].Started.Before(found[j].Started)
		}
		return found[i].PID < found[j].PID
	})
	return &found[0], nil
}
