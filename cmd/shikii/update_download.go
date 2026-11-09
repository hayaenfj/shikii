package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func updateVersion(value string) ([3]uint64, error) {
	var result [3]uint64
	parts := strings.Split(strings.TrimPrefix(value, "v"), ".")
	if len(parts) != 3 {
		return result, errors.New("invalid version")
	}
	for i, p := range parts {
		if p == "" || (len(p) > 1 && p[0] == '0') {
			return result, errors.New("invalid version")
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return result, errors.New("invalid version")
			}
		}
		n, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			return result, err
		}
		result[i] = n
	}
	return result, nil
}

func updateNewer(a, b [3]uint64) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func updateGet(ctx context.Context, o updateOptions, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.New("cannot parse the release source URL")
	}
	req.Header.Set("User-Agent", "shikii/"+o.version)
	req.Header.Set("Accept", "application/vnd.github+json")
	response, err := o.client.Do(req)
	if err != nil {
		return nil, errors.New("communication with the release source failed. The existing executable is kept")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cannot download from the release source (HTTP %d). Try again later", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, errors.New("cannot download the complete release")
	}
	if int64(len(data)) > limit {
		return nil, errors.New("the release source's response exceeds the size limit")
	}
	return data, nil
}

func updateDigest(data []byte, digest string) bool {
	if digest == "" {
		return true
	} // older GitHub assets have no digest. Checking SHA256SUMS is always required.
	sum := sha256.Sum256(data)
	return digest == "sha256:"+hex.EncodeToString(sum[:])
}

func updateChecksum(data []byte, name string) (string, error) {
	result := ""
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return "", errors.New("cannot recognize the SHA256SUMS format")
		}
		if strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		digest, err := hex.DecodeString(fields[0])
		if err != nil || len(digest) != sha256.Size || result != "" {
			return "", errors.New("cannot find a unique hash for the release")
		}
		result = strings.ToLower(fields[0])
	}
	if result == "" {
		return "", errors.New("the release hash is not in SHA256SUMS")
	}
	return result, nil
}

var updateFiles = []string{"LICENSE", "LICENSE-APACHE", "LICENSE-MIT", "THIRD-PARTY-NOTICES", "shikii-readout", "shikii"}
