package source

import (
	"cmp"
	"context"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	gittransport "github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage/memory"
)

const gitRemoteName = "origin"

type gitClient struct {
	cfg  GitConfig
	auth Auth
}

// Discover lists remote refs and returns matching versions, newest first.
func (c *gitClient) Discover(ctx context.Context) ([]Version, error) {
	auth, err := c.authMethod()
	if err != nil {
		return nil, err
	}

	switch {
	case c.cfg.SemverConstraint != "":
		refs, err := c.lsRemote(ctx, auth)
		if err != nil {
			return nil, err
		}
		return c.discoverSemver(refs)
	case c.cfg.TagPattern != "":
		refs, err := c.lsRemote(ctx, auth)
		if err != nil {
			return nil, err
		}
		return c.discoverTagPattern(refs)
	default:
		return nil, fmt.Errorf("git source: no selection strategy set")
	}
}

func (c *gitClient) lsRemote(ctx context.Context, auth gittransport.AuthMethod) ([]*plumbing.Reference, error) {
	remote := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: gitRemoteName,
		URLs: []string{c.cfg.Repository},
	})
	refs, err := remote.ListContext(ctx, &git.ListOptions{Auth: auth})
	if err != nil {
		return nil, fmt.Errorf("git source: ls-remote: %w", err)
	}
	return refs, nil
}

// discoverSemver returns tags satisfying the constraint, newest first.
func (c *gitClient) discoverSemver(refs []*plumbing.Reference) ([]Version, error) {
	constraint, err := semver.NewConstraint(c.cfg.SemverConstraint)
	if err != nil {
		return nil, fmt.Errorf("git source: semverConstraint %q: %w", c.cfg.SemverConstraint, err)
	}

	type match struct {
		v   Version
		ver *semver.Version
	}
	var matches []match
	for _, ref := range refs {
		if !ref.Name().IsTag() {
			continue
		}
		tag := ref.Name().Short()
		sv, err := semver.NewVersion(semverPart(tag))
		if err != nil {
			continue
		}
		if constraint.Check(sv) {
			matches = append(matches, match{v: Version{Version: tag, Revision: ref.Hash().String()}, ver: sv})
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("git source: no tags satisfy %q in %s", c.cfg.SemverConstraint, c.cfg.Repository)
	}
	slices.SortFunc(matches, func(a, b match) int { return b.ver.Compare(a.ver) })

	out := make([]Version, len(matches))
	for i, m := range matches {
		out[i] = m.v
	}
	return out, nil
}

// discoverTagPattern returns tags matching a glob, newest first.
func (c *gitClient) discoverTagPattern(refs []*plumbing.Reference) ([]Version, error) {
	type match struct {
		v   Version
		ver *semver.Version
	}
	var matches []match
	for _, ref := range refs {
		if !ref.Name().IsTag() {
			continue
		}
		tag := ref.Name().Short()
		ok, err := path.Match(c.cfg.TagPattern, tag)
		if err != nil {
			return nil, fmt.Errorf("git source: tagPattern %q: %w", c.cfg.TagPattern, err)
		}
		if !ok {
			continue
		}
		sv, _ := semver.NewVersion(semverPart(tag))
		matches = append(matches, match{v: Version{Version: tag, Revision: ref.Hash().String()}, ver: sv})
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("git source: no tags match %q in %s", c.cfg.TagPattern, c.cfg.Repository)
	}
	slices.SortFunc(matches, func(a, b match) int {
		if a.ver != nil && b.ver != nil {
			return b.ver.Compare(a.ver)
		}
		return cmp.Compare(b.v.Version, a.v.Version)
	})

	out := make([]Version, len(matches))
	for i, m := range matches {
		out[i] = m.v
	}
	return out, nil
}

// semverPart returns the trailing segment of a prefixed tag (perses/0.17.1 ->
// 0.17.1) so it can be parsed as a semver.
func semverPart(tag string) string {
	if i := strings.LastIndex(tag, "/"); i >= 0 {
		return tag[i+1:]
	}
	return tag
}

// Fetch clones the single tag ref shallow into memory and returns the bytes of
// filePath at that ref.
func (c *gitClient) Fetch(ctx context.Context, ref, filePath string) ([]byte, error) {
	auth, err := c.authMethod()
	if err != nil {
		return nil, err
	}

	refName := plumbing.NewTagReferenceName(ref)
	repo, err := git.CloneContext(ctx, memory.NewStorage(), nil, &git.CloneOptions{
		URL:           c.cfg.Repository,
		Auth:          auth,
		ReferenceName: refName,
		SingleBranch:  true,
		Depth:         1,
		Tags:          git.NoTags,
	})
	if err != nil {
		return nil, fmt.Errorf("git source: clone %s at %s: %w", c.cfg.Repository, ref, err)
	}

	head, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("git source: head at %s: %w", ref, err)
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return nil, fmt.Errorf("git source: commit %s: %w", head.Hash(), err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("git source: tree at %s: %w", ref, err)
	}
	file, err := tree.File(filePath)
	if err != nil {
		return nil, fmt.Errorf("git source: file %q at %s: %w", filePath, ref, err)
	}
	contents, err := file.Contents()
	if err != nil {
		return nil, fmt.Errorf("git source: read %q at %s: %w", filePath, ref, err)
	}
	return []byte(contents), nil
}
