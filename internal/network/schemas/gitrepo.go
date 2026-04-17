// Copyright 2026 Adrien Ndikumana
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package schemas

import (
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

// GitAuth holds credentials for authenticating to a Git remote.
type GitAuth struct {
	// Type is "none", "token", or "ssh".
	Type string
	// Token is a personal access token (used for HTTPS).
	Token string
	// SSHKeyPath is the path to an SSH private key.
	SSHKeyPath string
	// SSHKeyPassphrase is the passphrase for the SSH key (may be empty).
	SSHKeyPassphrase string
}

// transportAuth converts GitAuth into a go-git transport.AuthMethod.
func (a GitAuth) transportAuth() (transport.AuthMethod, error) {
	switch strings.ToLower(a.Type) {
	case "", "none":
		return nil, nil
	case "token":
		// GitHub/GitLab/BitBucket all accept token as password with any username.
		return &http.BasicAuth{
			Username: "x-token-auth",
			Password: a.Token,
		}, nil
	case "ssh":
		keys, err := ssh.NewPublicKeysFromFile("git", a.SSHKeyPath, a.SSHKeyPassphrase)
		if err != nil {
			return nil, fmt.Errorf("loading SSH key %q: %w", a.SSHKeyPath, err)
		}
		return keys, nil
	default:
		return nil, fmt.Errorf("unsupported git auth type %q", a.Type)
	}
}

// cacheKey returns a deterministic directory name for a (url, ref) pair.
func cacheKey(url, ref string) string {
	h := sha256.Sum256([]byte(url + "\x00" + ref))
	// Use the first 12 hex chars + sanitised ref for human readability.
	safe := strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(ref)
	return fmt.Sprintf("%x-%s", h[:6], safe)
}

// CloneOrPull ensures a local checkout of the given repo URL at the specified
// Git ref exists under cacheDir.  On first call it performs a shallow clone;
// on subsequent calls it fetches + resets to the latest remote state.
//
// Returns the absolute path to the checked-out worktree.
func CloneOrPull(url, cacheDir, ref string, auth GitAuth) (string, error) {
	dir := filepath.Join(cacheDir, cacheKey(url, ref))

	transport, err := auth.transportAuth()
	if err != nil {
		return "", fmt.Errorf("git auth: %w", err)
	}

	refName := plumbing.NewBranchReferenceName(ref)
	tagRefName := plumbing.NewTagReferenceName(ref)

	// If the directory already exists, try to pull.
	if info, statErr := os.Stat(filepath.Join(dir, ".git")); statErr == nil && info.IsDir() {
		return pullExisting(dir, url, ref, refName, tagRefName, transport)
	}

	// Fresh clone.
	return cloneFresh(dir, url, ref, refName, tagRefName, transport)
}

func cloneFresh(dir, url, ref string, refName, tagRefName plumbing.ReferenceName, auth transport.AuthMethod) (string, error) {
	slog.Info("cloning YANG repo", "url", url, "ref", ref, "dir", dir)

	// Try as branch first.
	_, err := git.PlainClone(dir, false, &git.CloneOptions{
		URL:           url,
		Auth:          auth,
		ReferenceName: refName,
		SingleBranch:  true,
		Depth:         1,
		Tags:          git.NoTags,
	})
	if err == nil {
		return dir, nil
	}

	// Branch clone failed — try as tag.
	_ = os.RemoveAll(dir)
	_, err = git.PlainClone(dir, false, &git.CloneOptions{
		URL:           url,
		Auth:          auth,
		ReferenceName: tagRefName,
		SingleBranch:  true,
		Depth:         1,
		Tags:          git.NoTags,
	})
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("cloning %s at ref %q: %w", url, ref, err)
	}

	return dir, nil
}

func pullExisting(dir, url, ref string, refName, tagRefName plumbing.ReferenceName, auth transport.AuthMethod) (string, error) {
	slog.Info("updating cached YANG repo", "url", url, "ref", ref, "dir", dir)

	repo, err := git.PlainOpen(dir)
	if err != nil {
		return "", fmt.Errorf("opening cached repo %s: %w", dir, err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("worktree for %s: %w", dir, err)
	}

	// Fetch the ref (try branch, then tag).
	fetchErr := repo.Fetch(&git.FetchOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{config.RefSpec(fmt.Sprintf("+%s:%s", refName, refName))},
		Auth:       auth,
		Depth:      1,
		Tags:       git.NoTags,
	})
	if fetchErr != nil && fetchErr != git.NoErrAlreadyUpToDate {
		// Try tag refspec.
		fetchErr = repo.Fetch(&git.FetchOptions{
			RemoteName: "origin",
			RefSpecs:   []config.RefSpec{config.RefSpec(fmt.Sprintf("+%s:%s", tagRefName, tagRefName))},
			Auth:       auth,
			Depth:      1,
			Tags:       git.NoTags,
		})
	}

	if fetchErr != nil && fetchErr != git.NoErrAlreadyUpToDate {
		slog.Warn("git fetch failed, using stale cache", "url", url, "ref", ref, "err", fetchErr)
		return dir, nil // stale cache fallback
	}

	// Reset worktree to fetched ref.
	resolvedRef, err := resolveRef(repo, refName, tagRefName)
	if err != nil {
		slog.Warn("could not resolve ref after fetch, using stale cache", "ref", ref, "err", err)
		return dir, nil
	}

	if err := wt.Reset(&git.ResetOptions{
		Commit: resolvedRef,
		Mode:   git.HardReset,
	}); err != nil {
		slog.Warn("git reset failed, using stale cache", "ref", ref, "err", err)
	}

	return dir, nil
}

// resolveRef tries to resolve a branch ref first, then a tag ref.
func resolveRef(repo *git.Repository, branchRef, tagRef plumbing.ReferenceName) (plumbing.Hash, error) {
	if ref, err := repo.Reference(branchRef, true); err == nil {
		return ref.Hash(), nil
	}
	if ref, err := repo.Reference(tagRef, true); err == nil {
		return ref.Hash(), nil
	}
	return plumbing.ZeroHash, fmt.Errorf("unable to resolve ref %s or %s", branchRef, tagRef)
}
