package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/sirdesmond/paved-road-platform/platform-api/internal/api"
)

func main() {
	var addr, owner, repo, baseBranch string
	flag.StringVar(&addr, "addr", ":8080", "Address to listen on.")
	flag.StringVar(&owner, "github-owner", "sirdesmond", "GitHub owner of the GitOps repo.")
	flag.StringVar(&repo, "github-repo", "paved-road-platform", "GitOps repository name.")
	flag.StringVar(&baseBranch, "base-branch", "main", "Branch to open pull requests against.")
	flag.Parse()

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		log.Fatal("GITHUB_TOKEN is not set. Use a fine-grained PAT scoped to this repo " +
			"with contents and pull-requests write.")
	}

	// Cluster access is for the read endpoints only. If it fails, carry on
	// without it: create still works, and a platform that half-works beats one
	// that refuses to start.
	k8s, err := api.NewK8sClient()
	if err != nil {
		log.Printf("WARNING: no cluster access (%v). list and status will return 503; create is unaffected.", err)
	}

	srv := api.NewServer(api.NewGitHub(token, owner, repo, baseBranch), k8s)

	log.Printf("platform-api listening on %s, opening PRs against %s/%s@%s", addr, owner, repo, baseBranch)
	if err := http.ListenAndServe(addr, srv.Routes()); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
