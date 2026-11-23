package main

// A session whose agent process is alive is not always at work: the agent stays open after a request ends. forget takes such a session
// when all of these are so, and refuses otherwise:
//
//   - it is not in the middle of a turn: the state of the session's turn (sessionLatest.InTurn) is opened by any event of the session
//     that reached the hook (a message of the person, a tool call, a subagent beginning or ending: sessionActivity) and closed by a Stop
//     that lets the turn end and that no event came after. A person who interrupts a turn may leave no Stop, and a command of the agent's
//     app that calls the hook (/clear) may leave none; that session is refused until its agent is closed. A state that holds an event
//     newer than the Stop that closed it is refused too.
//   - no subagent it started is still open: a subagent in the background thinks inside the agent's own process, where no process below
//     the agent says so, so the state keeps each one from its beginning to its end (sessionLatest.Subagents).
//   - nothing the session may have started is still running: no process under the agent that started after the hook first saw the agent
//     run the session (Noted). Processes of the agent that started before are its own (the servers it starts with itself); forget
//     itself, what it started and everything above it are not counted either.
//
// What cannot be read (no state of the turn, no time the agent was noted at, a process list that cannot be read) is not idle: forget
// refuses and says why. An application looks at the state of the turn again before each step (turnChanged) and stops when a session
// began something since the plan was made.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

// turnRead is what the state of the session's turn said when it was read (sessionLatest, with whether there is one).
type turnRead struct {
	latest sessionLatest
	known  bool
	err    error
}

// readTurn reads the state of the session's turn. While the plan is made (not applied) it keeps what it read: an application looks at the
// state again before each step and stops when it is no longer that (turnChanged).
func (r *forgetRun) readTurn(session string) turnRead {
	latest, known, err := readSessionLatest(session)
	t := turnRead{latest, known, err}
	if !r.applying {
		r.turnSeen[session] = t.canon()
	}
	return t
}

// canon is the state as one text, to see whether it changed.
func (t turnRead) canon() string {
	b, _ := json.Marshal(struct {
		Latest sessionLatest
		Known  bool
		Err    string
	}{t.latest, t.known, fmt.Sprint(t.err)})
	return string(b)
}

// turnChanged says whether the state of the turn of any session the plan went by is no longer what the plan read: a request began, a
// subagent began, the agent made a tool call, an event came. The plan was of a session that was not at work, and an application that goes
// on is not of that.
func (r *forgetRun) turnChanged() error {
	for _, session := range forgetSorted(forgetKeys(r.turnSeen)) {
		latest, known, err := readSessionLatest(session)
		if (turnRead{latest, known, err}).canon() != r.turnSeen[session] {
			return fmt.Errorf("%w: %s is at work again, so the rest was not run", errForgetTurnChanged, session)
		}
	}
	return nil
}

// aliveWhy says why a session whose agent process p is alive is still at work, or "" when it is idle. subject names the id as the
// refusal says it, and t is the state of the turn of the session that counts (a subagent's id is judged by the session it ran under).
func (r *forgetRun) aliveWhy(subject string, p *agentProcess, t turnRead) string {
	agent := fmt.Sprintf("its agent process (%s, pid %d) is alive", p.Name, p.PID)
	latest := t.latest
	switch {
	case t.err != nil:
		return r.cannotTell(subject, agent, "the state of its last turn could not be read: "+t.err.Error())
	case !t.known:
		return r.cannotTell(subject, agent, "no state of its last turn is kept: it is from an older shikii")
	case latest.InTurn == nil:
		return r.cannotTell(subject, agent, "its last turn was kept by an older shikii, which did not note whether it ended")
	case *latest.InTurn:
		return fmt.Sprintf("%s is still running: %s and its last turn has not ended (a request or a command began it, and no Stop that ends it was recorded); wait for the agent to finish it, or end that agent, then try again", subject, agent)
	case p.Noted.IsZero():
		return r.cannotTell(subject, agent, "its record does not say when the agent was first seen running it: it is from an older shikii")
	case latest.EventAt.After(latest.ClosedAt):
		return fmt.Sprintf("%s is still running: %s and an event of the session came after the Stop that ended its last turn (the turn went on); wait for the agent to finish it, or end that agent, then try again", subject, agent)
	}
	if id, at := openSubagent(latest, p.Noted); id != "" {
		return fmt.Sprintf("%s is still running: %s and a subagent it started is still open (%s, begun %s); a subagent in the background works inside the agent, so wait for it to end, or end that agent, then try again",
			subject, agent, id, at.UTC().Format(time.RFC3339))
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

// openSubagent is the oldest subagent the state says began after the agent was noted and has not ended ("" when there is none). One that
// began before belongs to an earlier process of the agent (the session was resumed): its end was never going to be told to this one.
func openSubagent(latest sessionLatest, noted time.Time) (id string, at time.Time) {
	for sub, began := range latest.Subagents {
		if began.After(noted) && (id == "" || began.Before(at) || began.Equal(at) && sub < id) {
			id, at = sub, began
		}
	}
	return id, at
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
