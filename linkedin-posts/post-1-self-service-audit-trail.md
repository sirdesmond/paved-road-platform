# Post 1 — Self-service without losing the audit trail

**Status:** ready to publish (from RFC-0001)
**Source:** `docs/rfc/0001-self-service-environments.md`
**Visual:** the sequence diagram from the RFC, or the Environment YAML.

---

## Final copy

The quickest way to build self-service provisioning is to let your API talk straight to the cluster. Request comes in, resources get created, everyone's happy.

I went the other way, and it's the decision I get argued with about most.

When someone asks for an environment, the API checks the request against policy, works out whether there's capacity, generates the manifests and opens a pull request. Argo CD takes it from there.

So yes, there's now a PR sitting in the middle of a workflow that was supposed to be fast. I know how that sounds.

But it's worth being clear about what direct provisioning quietly costs you. You lose the record of who asked for what. You lose the diff, so nobody can look at a change later and understand it. And undoing something stops being a revert and becomes detective work.

None of that hurts at all, right up until it really does.

The speed complaint is fair, though, so I'd rather fix it than argue about it. Dev and staging merge themselves as soon as the checks pass, which means the PR is a record rather than a queue. Production keeps a human reviewer, because that's the one place I want someone to actually look.

So it's a couple of minutes for the environments people spin up constantly, and a full history for everything.

The way I think about it: self-service should change who's allowed to start a change. It shouldn't change whether you can see what happened afterwards.

If you've done it the other way and it held up, I'd like to hear it. I might be overweighting the incident I'm thinking of.

#PlatformEngineering #GitOps #Kubernetes #DeveloperExperience #ArgoCD

—
I'm building an internal developer platform in the open. The write-ups and decisions are in the repo.

---

## First comment

> Part 1 of "Paved Roads". Full write-up is in the repo, including the options I ruled out (portal first, Crossplane, teams running Terraform themselves). Part 0 here: [link].

---

## Notes

- Strongest post in the set. It's a real call, and reasonable people disagree with it.
- Saying "I know how that sounds" before the objection lands does more work than any amount of justification after.
- The last line admits you might be biased by one incident. Leave it in. It invites replies instead of shutting them down, and it's true of most strong opinions in infra.
- The design isn't built yet. Nothing in the post says it is, so no issue, but don't let a comment thread drift into implying otherwise.
