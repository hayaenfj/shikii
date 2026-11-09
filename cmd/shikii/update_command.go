package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	shikii "github.com/hayaenfj/shikii"
)

func cmdUpdate(args []string) error {
	if len(args) != 0 {
		return errors.New("shikii update takes no arguments or options")
	}
	exe, err := os.Executable()
	if err != nil {
		return errors.New("cannot determine where the executable is")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return runUpdate(ctx, defaultUpdateOptions(exe))
}

func defaultUpdateOptions(exe string) updateOptions {
	client := &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 || req.URL.Scheme != "https" {
			return errors.New("cannot verify the redirect to the release source")
		}
		switch req.URL.Hostname() {
		case "api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "github-releases.githubusercontent.com":
			return nil
		default:
			return errors.New("cannot verify the redirect to the release source")
		}
	}}
	return updateOptions{
		executable: exe, version: shikii.Version, goos: runtime.GOOS, goarch: runtime.GOARCH,
		latestURL: "https://api.github.com/repos/hayaenfj/shikii/releases/latest", downloadBase: "https://github.com", client: client,
		finish: finishUpdate, reset: resetProjectStores,
	}
}

type updateRelease struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

func runUpdate(ctx context.Context, o updateOptions) error {
	if (o.goos != "darwin" && o.goos != "linux") || (o.goarch != "arm64" && o.goarch != "amd64") {
		return fmt.Errorf("no release is provided for this platform (%s/%s)", o.goos, o.goarch)
	}
	current, err := updateVersion(o.version)
	if err != nil {
		return errors.New("cannot determine the current version")
	}
	exe, err := filepath.EvalSymlinks(o.executable)
	if err != nil {
		return errors.New("cannot resolve the real executable to update")
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Dir(exe))
	if err != nil {
		return errors.New("cannot open the executable's folder")
	}
	defer root.Close()
	fmt.Fprintln(stdout, "Checking for the latest version…")
	data, err := updateGet(ctx, o, o.latestURL, 1<<20)
	if err != nil {
		return err
	}
	var release updateRelease
	if err := json.Unmarshal(data, &release); err != nil {
		return errors.New("cannot read the latest release information")
	}
	next, err := updateVersion(release.Tag)
	if err != nil || !strings.HasPrefix(release.Tag, "v") || release.Draft || release.Prerelease {
		return errors.New("cannot find stable release information")
	}
	if next == current || !updateNewer(next, current) {
		if next == current {
			fmt.Fprintf(stdout, "shikii %s is the latest version. Nothing changed.\n", o.version)
		} else {
			fmt.Fprintf(stdout, "Local shikii %s is newer than the published stable release %s, so nothing is changed.\n", o.version, strings.TrimPrefix(release.Tag, "v"))
		}
		return nil
	}
	version := strings.TrimPrefix(release.Tag, "v")
	lock, err := updateLock(root)
	if err != nil {
		return err
	}
	defer lock.Close()
	filename := fmt.Sprintf("shikii_%s_%s_%s.tar.gz", version, o.goos, o.goarch)
	assets := map[string]string{}
	digests := map[string]string{}
	for _, asset := range release.Assets {
		if asset.Name != filename && asset.Name != "SHA256SUMS" {
			continue
		}
		expected := o.downloadBase + "/hayaenfj/shikii/releases/download/" + release.Tag + "/" + asset.Name
		if asset.URL != expected || assets[asset.Name] != "" {
			return errors.New("cannot verify the official release URL")
		}
		assets[asset.Name], digests[asset.Name] = asset.URL, asset.Digest
	}
	if assets[filename] == "" || assets["SHA256SUMS"] == "" {
		return errors.New("the release for this platform and SHA256SUMS are not both available")
	}
	sums, err := updateGet(ctx, o, assets["SHA256SUMS"], 512<<10)
	if err != nil {
		return err
	}
	if !updateDigest(sums, digests["SHA256SUMS"]) {
		return errors.New("the SHA256SUMS hash does not match")
	}
	expectedHash, err := updateChecksum(sums, filename)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Downloading shikii %s…\n", version)
	archive, err := updateGet(ctx, o, assets[filename], 128<<20)
	if err != nil {
		return err
	}
	actual := sha256.Sum256(archive)
	if hex.EncodeToString(actual[:]) != expectedHash || !updateDigest(archive, digests[filename]) {
		return errors.New("the release hash does not match. The existing executable was kept")
	}
	files, err := updateArchive(archive)
	if err != nil {
		return err
	}
	if err := installUpdate(ctx, root, exe, version, files, o.replace); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Updated shikii %s → %s.\n", o.version, version)
	if o.reset != nil {
		if err := o.reset(ctx, exe, current, next); err != nil {
			return fmt.Errorf("shikii and readout were updated, but re-initializing the projects could not be completed: %w", err)
		}
	}
	if o.finish != nil {
		if err := o.finish(ctx, exe); err != nil {
			return fmt.Errorf("shikii and readout were updated, but applying the update could not be completed: %w", err)
		}
	}
	return nil
}
