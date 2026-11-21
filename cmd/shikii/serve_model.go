package main

import "net/http"

// handleModel sets the model this project's decisions use, from the page: {"model": "hosted" | "local" | ""}. "" follows
// SHIKII_MODEL again. The choice is the same one shikii init --model writes, and the next decision of every hook, decide and page of
// this project follows it.
func (s *server) handleModel(w http.ResponseWriter, r *http.Request) {
	if !acceptJSONPost(w, r) {
		return
	}
	var req struct {
		Model string `json:"model"`
	}
	if err := decodeObject(r.Body, &req); err != nil {
		writeBodyErr(w, err)
		return
	}
	if err := setProjectModel(s.o.storeDir, req.Model); err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]any{"choice": projectModel(s.o.storeDir), "uses_hosted": usesHosted(s.o.storeDir)})
}
