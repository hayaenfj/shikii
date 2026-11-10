package main

import (
	"bytes"
	"encoding/json"
	"io"
)

// Return decoding and trailing-data results separately so each entry preserves its errors.
func decodeStrictObject(data []byte, target any) (err error, extra bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err, false
	}
	var trailing any
	return nil, dec.Decode(&trailing) != io.EOF
}
