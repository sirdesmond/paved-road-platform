# Post 5 — Shift down, and the bill nobody mentions

**Status:** ready to publish. **Intended as post two**, following the deadlock story.
**Depends on:** [post 4](./post-4-admission-deadlock.md) being published first — the callback does real work.
**Visual:** none. This one is an argument, and a diagram would dilute it.

---

## Final copy

Two ideas from platform engineering that get quoted separately and almost never together.

Shift down: stop pushing infrastructure work left onto developers and absorb it into the platform instead.
Google Cloud have been making this case for a while and I think they're right.

Platform as a product: your developers are customers, adoption is the measure, and nobody is obliged to use
what you built. That one goes back to Team Topologies, though Databricks' IT team wrote a good piece on
running it for real.

Put them side by side and something awkward falls out.

When you shift complexity down, you don't delete it. You move it somewhere your users can't see. Which means
you've also taken away their ability to work out what went wrong.

I ran into this last week, and wrote about it here. Two guardrails I'd built, each sensible on its own,
combining to make an object impossible to delete. No developer caused it. No developer could have diagnosed
it. I had successfully removed their cognitive load and replaced it with total dependence on me.

That's the bill for shifting down, and I don't see it discussed much. The load doesn't disappear. It converts
into an obligation.

If product thinking means anything here, it means the failure experience is part of the product. Error
messages that name the actual limit and who to ask, rather than "invalid request". Status that says what's
wrong instead of that something is.

Take complexity away from people and you inherit the duty to explain it.

Where do you draw that line? How much should a platform hide before hiding becomes the problem?

#PlatformEngineering #DeveloperExperience #SRE #Kubernetes

—
I'm building an internal developer platform in the open. The write-ups and decisions are in the repo.

---

## First comment

> Part two of a few. The failure I'm referring to is here: [link to post 4]. Full write-up including the CEL fix is in the repo.

---

## Before you post

- **Publish post 4 first.** "I ran into this last week, and wrote about it here" is doing the heavy lifting — it's what makes this an argument from experience rather than a synthesis of other people's ideas.
- **Attribution matters in this one.** Shift down to Google Cloud, platform-as-product to Team Topologies with Databricks as a practitioner example. Getting the lineage wrong in front of this audience costs more than the post gains.
- Keep both framings to one line each. The moment either becomes a paragraph of explanation, it turns into a book report on other people's work.

## Notes

- The load quote is the shareable unit: **"The load doesn't disappear. It converts into an obligation."** Keep it on its own line.
- The closing question is a genuine one with no correct answer, which is what makes it good bait for practitioners — everyone who has run a platform has an opinion about how much abstraction is too much.
- Expect pushback along the lines of "that's just good observability". Agree, and push further: observability tells *you* what happened; the point here is that your users need it too, and they no longer have the context to interpret it.
- If it lands, the natural third post is the Terraform boundary one — concrete, opinionated, and a change of pace from two conceptual posts in a row.
