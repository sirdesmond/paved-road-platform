# Post 2 — Where Terraform stops

**Status:** ready to publish (from ADR-0001)
**Source:** `docs/adr/0001-terraform-vs-controller-boundary.md`
**Visual:** two columns, what Terraform owns vs what the controller owns.

---

## Final copy

Sooner or later every platform team argues about how much belongs in Terraform.

In my experience nobody actually decides. It just accumulates, and then one day someone asks for something completely routine and it kicks off a forty minute apply that a person has to sit and watch.

I'd rather write the line down early, before it gets drawn by accident.

Terraform gets the things that change a few times a year. Accounts, networking, clusters, IAM, node groups. Big blast radius, worth a plan and a second pair of eyes.

The platform's own controller gets the things that change constantly. Namespaces, quotas, network policies, routes, monitoring defaults. Created and deleted all the time, and expected to put itself right when it drifts.

When something's ambiguous I fall back on this: if a team's request is what triggers the change, it probably shouldn't be in Terraform.

That answers most of it. Nobody should be waiting behind a state lock because they wanted a staging environment. But replacing a node group should be slow and visible, and I don't want that automated away.

The cost is that you're running two things instead of one and people have to know which is which. I'll take that trade, mostly because the rule is short enough that people remember it without being told twice.

The edge cases are the interesting bit. A bucket that belongs to one team and gets deleted with their environment? I'd put that with the controller, though I've changed my mind on it before.

Where do you draw it? I've seen far more teams regret pushing too much into Terraform than too little.

#PlatformEngineering #Terraform #Kubernetes #GitOps #InfrastructureAsCode

—
I'm building an internal developer platform in the open. The write-ups and decisions are in the repo.

---

## First comment

> Part 2 of "Paved Roads". The write-up is in the repo, including why I didn't just put all of it in Crossplane. Part 1 here: [link].

---

## Notes

- Likely the most commented post in the series. It's a live argument in the industry and the ending asks for disagreement rather than agreement.
- "I've changed my mind on it before" is the human bit. Keep it.
- The forty minute apply detail is what makes people recognise their own team. Specifics beat abstractions.
