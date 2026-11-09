package notebook

import (
	"bytes"
	"errors"
	"io"
)

// File size is only an allocation hint. A concurrent resize must not truncate
// a successful read or bypass the fixed byte limit.
func readPageBytes(r io.Reader, sizeHint int64) ([]byte, error) {
	const limit = 64 << 10
	var out bytes.Buffer
	out.Grow(int(max(0, min(sizeHint, limit))) + bytes.MinRead)
	_, err := out.ReadFrom(io.LimitReader(r, limit+1))
	data := out.Bytes()
	if err == nil && len(data) > limit {
		err = errors.New("wiki page exceeds the size limit")
	}
	return data, err
}
