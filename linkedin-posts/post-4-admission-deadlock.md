# Post 4 — The deadlock between two guardrails

**Status:** ready to publish. **Strong candidate to lead with.**
**Source:** [runbook 0004](../docs/runbooks/0004-admission-policy-deadlocks-finalizer.md)
**Visual:** none needed. A terminal screenshot of the reconciler error works if you want one, but the story
carries itself and text-only posts do fine.

---

## Final copy

I built two guardrails this week. Each one worked exactly as designed. Together they made an object
impossible to delete.

The setup, briefly. My platform hands teams an isolated environment, and each one owns a namespace full of
their stuff. There's a finalizer on it so cleanup runs before the object disappears. Separately there's an
admission policy that rejects environments asking for more CPU than their tier allows.

Both sensible. Both tested. I even did the responsible thing and ran the policy in audit mode first, watched
what it would have blocked, then turned on enforcement.

Then I tried to delete an old environment that happened to violate the rule.

Deleting it means removing the finalizer. Removing a finalizer is an update. The update goes through
admission. The object still asks for 200 CPUs, so the policy rejects it. Which means the finalizer stays,
which means the object can't be deleted, which means my controller sat there retrying politely, forever.

The part that stuck with me is that audit mode couldn't have caught it. In audit the update succeeds. The
deadlock only exists once you enforce, and only for objects that were already broken before you did.

The fix was to judge the change rather than the state: allow an update if the value you care about hasn't
changed. New violations still get rejected, existing ones can still be cleaned up.

What I'd take from it: turning on a policy changes the meaning of every future write to a matching object,
not just writes to the thing you were worried about.

What's the most surprising thing you've had to un-break after switching enforcement on?

#Kubernetes #PlatformEngineering #SRE #DevOps

—
I'm building an internal developer platform in the open. The write-ups and decisions are in the repo.

---

## First comment

> Longer write-up with the CEL for the fix, and the diagnostic path that got me there, is in the repo. Happy to share the link if useful.

---

## Notes

- **Why this works as a first post:** nobody can write it but you. No prior context needed, no series to have followed, and it's a story rather than a claim.
- The confession that audit mode wouldn't have caught it is the strongest line. Resist the urge to soften it — publishing the limitation of the thing you did right is what separates this from a tutorial.
- The closing question invites war stories, and war stories are what people actually enjoy replying with. Expect better comments than an opinion post would get.
- Keep it text-only unless the screenshot genuinely adds something. A wall of terminal output shrinks to unreadable on a phone.
- If someone asks whether this is production: it's a lab build, say so plainly and move to the reasoning. Nobody has ever cared less about an answer than they will about that one.
