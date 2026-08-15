package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/go-github/v66/github"

	"github.com/sirdesmond/paved-road-platform/environment-controller/api/v1alpha1"
)

// GitHub opens pull requests against the GitOps repo. Self-service without
// giving up the audit trail: every environment arrives as a reviewable,
// revertible commit. See RFC-0001.
type GitHub struct {
	client     *github.Client
	owner      string
	repo       string
	baseBranch string
}

// NewGitHub builds a client from a token. The token comes from the
// environment, never from a file in the repo.
func NewGitHub(token, owner, repo, baseBranch string) *GitHub {
	return &GitHub{
		client:     github.NewClient(nil).WithAuthToken(token),
		owner:      owner,
		repo:       repo,
		baseBranch: baseBranch,
	}
}

// EnvironmentPath is where an environment's manifest lives in the GitOps repo.
// Defined once so the PR flow and any message that mentions it can't drift.
//
// Note the limitation: one file per team means one environment per team.
// Moving to environments/<team>/<name>.yaml would fix it, but existing files
// need renaming in the same change or you get two manifests defining the same
// object in one Application.
func EnvironmentPath(env *v1alpha1.Environment) string {
	return fmt.Sprintf("environments/%s/environment.yaml", env.Spec.Owner.Team)
}

func (g *GitHub) OpenEnvironmentPR(ctx context.Context, env *v1alpha1.Environment, manifest []byte) (string, error) {
	branch := "env/" + env.Name
	path := EnvironmentPath(env)

	if found, err := g.exists(ctx, path); err != nil {
		return "", fmt.Errorf("checking %s: %w", path, err)
	} else if found {
		return "", fmt.Errorf("%w: %s", ErrAlreadyExists, env.Name)
	}

	// 1. where the base branch is now
	base, _, err := g.client.Git.GetRef(ctx, g.owner, g.repo, "refs/heads/"+g.baseBranch)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", g.baseBranch, err)
	}

	// 2. a branch off it. Tolerate it already existing so a retried request
	//    doesn't fail with a confusing 422.
	_, _, err = g.client.Git.CreateRef(ctx, g.owner, g.repo, &github.Reference{
		Ref:    github.String("refs/heads/" + branch),
		Object: &github.GitObject{SHA: base.Object.SHA},
	})
	if err != nil && !strings.Contains(err.Error(), "Reference already exists") {
		return "", fmt.Errorf("creating branch %s: %w", branch, err)
	}

	// 3. the file. Content is raw bytes; go-github base64-encodes it.
	//
	// The trailer is the durable record: PR descriptions get edited and
	// repositories get migrated, but the commit message travels with the
	// history. `git log --grep 'Requested-by:'` is then a real audit query.
	requester := env.Annotations[AnnotationRequestedBy]
	commitMsg := fmt.Sprintf(
		"Add environment %s for %s\n\nRequested-by: %s\nRequested-via: platform-api\n",
		env.Name, env.Spec.Owner.Team, requester)

	_, _, err = g.client.Repositories.CreateFile(ctx, g.owner, g.repo, path,
		&github.RepositoryContentFileOptions{
			Message: github.String(commitMsg),
			Content: manifest,
			Branch:  github.String(branch),
		})
	if err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}

	// 4. the PR
	pr, _, err := g.client.PullRequests.Create(ctx, g.owner, g.repo, &github.NewPullRequest{
		Title: github.String(fmt.Sprintf("Environment: %s (%s)", env.Name, env.Spec.Tier)),
		Head:  github.String(branch),
		Base:  github.String(g.baseBranch),
		Body: github.String(fmt.Sprintf(
			"Requested by **%s** via platform-api.\n\n"+
				"- Environment: `%s`\n- Team: %s\n- Tier: %s\n- Contact: %s\n\n"+
				"> Attribution is self-declared: platform-api is currently unauthenticated, "+
				"so this records who *said* they were asking, not a verified identity.\n",
			requester, env.Name, env.Spec.Owner.Team, env.Spec.Tier, env.Spec.Owner.Contact)),
	})
	if err != nil {
		return "", fmt.Errorf("opening PR: %w", err)
	}
	return pr.GetHTMLURL(), nil
}

// in github.go
var ErrAlreadyExists = errors.New("environment already exists")

func (g *GitHub) exists(ctx context.Context, path string) (bool, error) {
	_, _, resp, err := g.client.Repositories.GetContents(ctx, g.owner, g.repo, path,
		&github.RepositoryContentGetOptions{Ref: g.baseBranch})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (g *GitHub) FindOpenPR(ctx context.Context, envName string) (string, error) {
	prs, _, err := g.client.PullRequests.List(ctx, g.owner, g.repo, &github.PullRequestListOptions{
		State: "open",
		Head:  g.owner + ":env/" + envName,
	})
	if err != nil || len(prs) == 0 {
		return "", err
	}
	return prs[0].GetHTMLURL(), nil
}
