package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestMCPNotificationCannotDeliverOrMutateWork(t *testing.T) {
	for _, id := range []json.RawMessage{nil, json.RawMessage("null")} {
		calls := 0
		target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			writeJSON(w, 200, map[string]any{"saved": true})
		})
		params := json.RawMessage(`{"name":"shikii_wiki","arguments":{"operation":"prepare","work":{"query":"synthetic","request":"Check an unacknowledged delivery."}}}`)
		reply := handleRPC(rpcRequest{JSONRPC: "2.0", ID: id, Method: "tools/call", Params: params}, target)
		if reply != nil || calls != 0 {
			t.Fatal("a tool notification has no response and cannot establish delivery or mutate work")
		}
	}
}
