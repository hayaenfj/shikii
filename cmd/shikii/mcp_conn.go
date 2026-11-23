package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// An MCP connection is one stream of JSON-RPC lines. Almost every tool answers a request with one reply, so the server handles one
// message at a time. A tool that must have a person decide (forget) sends the client a question, elicitation/create (MCP 2025-06-18),
// and reads on from the same stream until the answer with the question's id comes. The person's answer reaches shikii only through
// the client: no argument of a tool carries it, so an agent cannot answer for the person.

// mcpAskLimit is how long shikii waits for the person to answer a question the client shows.
var mcpAskLimit = 10 * time.Minute

// mcpLine is one read of the stream: the bytes up to a newline and the error that ended the read (io.EOF at the end).
type mcpLine struct {
	data []byte
	err  error
}

type mcpConn struct {
	lines  <-chan mcpLine // what the one reader of the stream read; nil for a request answered without a stream (handleRPC)
	enc    *json.Encoder
	elicit bool // the client said, when it initialized, that it can ask the person
	asked  int  // the questions sent, which number their ids
	ended  bool // the stream ended while a question was open
	endErr error
}

// canElicit reads the elicitation capability of the client: it is there, and it is an object that is empty (an empty one means a form,
// since 2025-11-25) or names form. A client that offers only a link to a page (url) cannot show a form.
func canElicit(raw json.RawMessage) bool {
	var modes map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &modes) != nil || modes == nil {
		return false
	}
	_, form := modes["form"]
	return len(modes) == 0 || form
}

func (c *mcpConn) canAsk() bool { return c.elicit && c.lines != nil && c.enc != nil }

func (c *mcpConn) write(v any) error { return c.enc.Encode(v) }

// serve handles one line read from the stream.
func (c *mcpConn) serve(data []byte, h http.Handler) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	var req rpcRequest
	if jerr := json.Unmarshal(data, &req); jerr != nil {
		_ = c.write(map[string]any{"jsonrpc": "2.0", "id": nil, "error": rpcError{-32700, "parse error: " + jerr.Error()}})
		return nil
	}
	if isRPCAnswer(req) {
		return nil // a late answer to a question nobody waits for any more (it timed out or was cancelled)
	}
	if resp := c.handle(req, h); resp != nil {
		return c.write(resp)
	}
	return nil
}

// isRPCAnswer reports whether the message answers a request the server sent (it names no method and holds a result or an error).
func isRPCAnswer(req rpcRequest) bool {
	return req.Method == "" && (len(req.Result) > 0 || len(req.Error) > 0)
}

// askKind is what a question to the person came to.
type askKind int

const (
	answerYes           askKind = iota // accepted, with remove true
	answerNo                           // declined, or accepted with remove false
	answerDismissed                    // the person dismissed the question (cancel)
	answerMalformed                    // an answer that is not the form that was asked
	answerFailed                       // the client answered the question with an error, or the question could not be sent
	answerNone                         // the time limit passed, or the stream ended, before an answer
	answerCallCancelled                // the client cancelled the tool call that asked
)

type mcpReply struct {
	kind   askKind
	detail string
}

// ask sends the question and waits for its answer. call is the id of the tool call that asks (the client may cancel it). Nothing
// else is run while it waits: a ping is answered, a notification passes, any other request is told that shikii is busy.
func (c *mcpConn) ask(call json.RawMessage, message string, schema map[string]any) mcpReply {
	c.asked++
	id, _ := json.Marshal(fmt.Sprintf("shikii-ask-%d", c.asked))
	err := c.write(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": "elicitation/create",
		"params": map[string]any{"message": message, "requestedSchema": schema}})
	if err != nil {
		return mcpReply{answerFailed, "the question could not be sent: " + err.Error()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), mcpAskLimit)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			c.cancelAsk(id, "no answer in time")
			return mcpReply{answerNone, "no answer came in time"}
		case l, open := <-c.lines:
			if !open {
				c.ended = true
				return mcpReply{answerNone, "the connection closed before an answer"}
			}
			if len(bytes.TrimSpace(l.data)) > 0 {
				if reply, done := c.whileAsking(l.data, call, id); done {
					c.noteEnd(l)
					return reply
				}
			}
			if l.err != nil {
				c.noteEnd(l)
				return mcpReply{answerNone, "the connection closed before an answer"}
			}
		}
	}
}

// noteEnd keeps that the stream ended in a read the wait made, so that the loop that handles the messages ends too.
func (c *mcpConn) noteEnd(l mcpLine) {
	if l.err == nil {
		return
	}
	c.ended = true
	if !errors.Is(l.err, io.EOF) {
		c.endErr = l.err
	}
}

func (c *mcpConn) cancelAsk(id json.RawMessage, reason string) {
	_ = c.write(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": json.RawMessage(id), "reason": reason}})
}

// whileAsking handles one message that comes while a question is open. It reports done with the reply when the message ends the wait.
func (c *mcpConn) whileAsking(data []byte, call, id json.RawMessage) (mcpReply, bool) {
	var req rpcRequest
	if jerr := json.Unmarshal(data, &req); jerr != nil {
		_ = c.write(map[string]any{"jsonrpc": "2.0", "id": nil, "error": rpcError{-32700, "parse error: " + jerr.Error()}})
		return mcpReply{}, false
	}
	switch {
	case isRPCAnswer(req):
		if !bytes.Equal(bytes.TrimSpace(req.ID), id) {
			return mcpReply{}, false // an answer to another request is no answer to this question
		}
		return readAnswer(req), true
	case req.Method == "notifications/cancelled":
		var p struct {
			RequestID json.RawMessage `json:"requestId"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if len(p.RequestID) > 0 && bytes.Equal(bytes.TrimSpace(p.RequestID), bytes.TrimSpace(call)) {
			c.cancelAsk(id, "the call that asked was cancelled")
			return mcpReply{answerCallCancelled, "the call was cancelled"}, true
		}
	case len(req.ID) == 0 || string(req.ID) == "null":
		// another notification: nothing to answer
	case req.Method == "ping":
		_ = c.write(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{}})
	case req.Method == "tools/call":
		busy := errors.New("busy: shikii is waiting for the person's answer to a question about removing records; call again after it is answered")
		_ = c.write(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": toolError(busy)})
	default:
		_ = c.write(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": rpcError{-32000, "busy: shikii is waiting for the person's answer to a question; try again after it is answered"}})
	}
	return mcpReply{}, false
}

// readAnswer reads the client's answer to the question: only accept with remove true is a yes. Anything else is not, and an answer
// that is not the form asked (no remove, a remove that is not a boolean, an action that is none of the three) is said to be malformed.
func readAnswer(req rpcRequest) mcpReply {
	if len(req.Error) > 0 {
		var e rpcError
		if json.Unmarshal(req.Error, &e) != nil || e.Message == "" {
			return mcpReply{answerFailed, "the client answered with an error"}
		}
		return mcpReply{answerFailed, "the client answered with an error: " + e.Message}
	}
	var res struct {
		Action  string          `json:"action"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(req.Result, &res) != nil {
		return mcpReply{answerMalformed, "the answer is not an object with an action"}
	}
	switch res.Action {
	case "decline":
		return mcpReply{answerNo, ""}
	case "cancel":
		return mcpReply{answerDismissed, ""}
	case "accept":
		var content map[string]json.RawMessage
		if json.Unmarshal(res.Content, &content) != nil {
			return mcpReply{answerMalformed, "the answer accepts but holds no remove"}
		}
		switch strings.TrimSpace(string(content["remove"])) {
		case "true":
			return mcpReply{answerYes, ""}
		case "false":
			return mcpReply{answerNo, ""}
		}
		return mcpReply{answerMalformed, "the answer accepts but its remove is not a boolean"}
	}
	return mcpReply{answerMalformed, "the answer's action is none of accept, decline or cancel"}
}
