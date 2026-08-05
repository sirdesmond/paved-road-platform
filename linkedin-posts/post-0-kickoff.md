# Post 0 — Kickoff: the platform team as a queue

**Status:** ready to publish, no demo needed
**Audience:** platform engineers, SREs, engineering leaders
**Visual:** the before/target table from the README, or the architecture diagram.

---

## Final copy

I keep running into platform teams who think they have a tooling problem. Most of the time they have a queue.

Somebody needs a new environment. The steps aren't complicated, but they live in the heads of two or three people, so the request sits there until one of them has a free afternoon. That's how something that's maybe an hour of real work turns into a week.

The awkward part is that none of it shows up as a problem. The waiting is invisible. The people doing the unblocking are usually the ones you'd rather have building the next thing, and nobody logs "answered the same provisioning question for the ninth time" as work.

Then there's the drift. Every environment set up by hand is slightly different from the last one, and you don't find out which differences mattered until something breaks at 2am.

What actually worries me is what happens as the company grows. The platform team quietly becomes the ceiling on how fast everyone else moves. Hiring more engineers doesn't help much if they all end up in the same queue.

So I'm building the other version of this, in the open. Teams create their own environments, the defaults are sensible, and the guardrails hold without anyone having to think about them.

The obvious shortcut is to hand everyone cluster access and be done with it. I've watched that play out. You trade a slow queue for a pile of resources nobody owns, and you pay for it later in incidents instead of upfront in waiting.

Most of this comes down to making the safe way also the fastest way. The rest is detail.

I'll post the decisions as I make them, including the ones I'm still unsure about.

What finally pushed you to build this properly?

#PlatformEngineering #DeveloperExperience #Kubernetes #SRE #InternalDeveloperPlatform

—
I'm building an internal developer platform in the open. The write-ups and decisions are in the repo.

---

## First comment

> Part 0 of "Paved Roads". Repo and roadmap are public, and I'll link the write-up behind each post. Next one is about why provisioning opens a pull request instead of just creating things.

---

## Notes

- No demo needed. It's an argument, not a claim about something running.
- The bit people will react to is the platform team becoming the ceiling on hiring. That's the line an engineering manager repeats to their boss.
- Keep the paragraph about handing out cluster access. Without it this reads like someone who's only seen one failure mode.
- If somebody asks whether it's in production, just say it's a lab build and you're publishing the reasoning as you go.
