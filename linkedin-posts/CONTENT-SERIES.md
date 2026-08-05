# Content series — "Paved Roads"

> Posts about building an internal developer platform: turning expert-driven provisioning into self-service
> systems teams trust. Aimed at **platform engineers and engineering leaders**.

**Series name:** **Paved Roads — notes from building an internal platform**

---

## How this series differs from the AI-infra one

| | Paved Roads (this) | Platforming Nuvaro (sibling repo) |
|---|---|---|
| **Audience** | Platform engineers, SREs, engineering leaders, hiring managers | Founders, AI leads |
| **Voice** | Decision-led and opinionated — "here's the call I made and why" | Transformation narrative — before → after → number |
| **Framing** | An engineering lab, spoken about honestly | A fictional client case study |
| **Post trigger** | A written decision (RFC/ADR) *or* a shipped milestone | A shipped milestone |
| **Emoji/structure** | Sober. Minimal formatting, no emoji section headers | Hook-driven with visual markers |

Don't cross-post between them. Different readers, different promises.

---

## The advantage: opinion posts are publishable today

Most build-in-public series stall waiting for demos. This one doesn't have to, because the project's
artifacts *are* written decisions. An RFC or ADR is publishable the day it's written — the reasoning is the
content, and reasoning is what an engineering leader is actually evaluating.

So the series has two post types:

- **Decision posts** (publishable now) — a real design call, the alternatives, the trade-off. Sourced from `docs/rfc/` and `docs/adr/`.
- **Milestone posts** (gated) — something works; show it. Terminal output, a timing, a dashboard.

Alternate between them. Decision posts keep cadence alive while the build catches up.

---

## Voice: assertive, not apologetic — and accurate

Write from conviction. These posts argue **positions about how platforms should work**, with this project as
evidence rather than as the subject. That framing is what reads as senior, and it's why the series needs no
client attribution to carry authority: a well-reasoned position other practitioners want to argue with is a
stronger credential than a logo.

Concretely:

- **State designs as positions you'd defend** — "here's the design I'd defend," "write the boundary down before it decides itself" — not "I'm learning how to…" or "I think maybe…".
- **Give advice in the imperative** where you believe it. Hedging reads as junior far more than being wrong does.
- **Name the cost of every decision.** Confidence without acknowledged trade-offs reads as a vendor pitch; confidence *with* them reads as experience.
- **Never claim delivery you can't substantiate.** Don't imply production scale, client engagements, or outcomes that didn't happen. Not mainly for ethics — because a Staff-level reader probes, and one claim that collapses under a follow-up question takes the rest of your credibility with it. You lose nothing by omitting a claim; you lose everything when one fails.

The two rules work together: **be maximally confident about your reasoning, and scrupulously accurate about
your results.** Reasoning is what you're actually being evaluated on.

**Standing footer:**

> Building an internal developer platform in the open. The RFCs and ADRs behind each post are in the repo.

If someone asks directly whether it's running in production: *"It's a lab build — the designs are ones I'd
defend in a review, and I'm shipping them in the open."* Confident, accurate, and it moves the conversation
to the reasoning, which is where you want it.

---

## Sounding like a person

Most infra content on LinkedIn reads like it came out of a machine, and readers now spot it instantly. The
tells, all of which the first drafts of these posts were guilty of:

- **Everything in threes.** Three bullets, three-word fragments, three parallel clauses. Real people write lists of two, or four, or an awkward five.
- **A punchline at the end of every paragraph.** Endless little mic drops. Nobody talks like that.
- **Em-dashes everywhere.** Use commas, full stops, or brackets. One dash a post is plenty.
- **Arrow bullets (→) stacked up.** Fine occasionally, but they're the house style of every AI-written LinkedIn post.
- **"It's not X. It's Y."** and its cousins. Once is a rhetorical device, three times is a tell.
- **Clever coinages** like "a strategic problem wearing an operational disguise". Reads impressive, feels synthetic.
- **Perfect symmetry.** Balanced pairs and neat antitheses in every paragraph.

What to do instead: vary sentence length properly, including some long ones that wander a bit. Use contractions.
Put in concrete details (a forty minute apply, a 2am page, an argument you lost). Admit uncertainty where it's
real. Let a paragraph end flat instead of landing a line. Say "I've changed my mind on this before" when you
have, because nobody who has actually run a platform is certain about all of it.

The simplest check: read it aloud. If you'd never say it to a colleague over coffee, rewrite it.

---

## Post shape

Not a template, because templates are how everything ends up sounding the same. Roughly:

Open with the situation, not a thesis statement. Say what you decided and why. Be honest about what you ruled
out and what the decision costs you. If there's a rule worth stealing, say it plainly, once. End with a real
question — ideally one you actually want an answer to.

Around 250 words. The first two lines matter most; LinkedIn hides the rest behind "see more".

---

## Post plan

### Publishable now (decision posts)

| # | Working title | Source | Angle |
|---|---|---|---|
| **0** | "Your platform team is a queue. That's the bug." | README / charter | Expert-driven provisioning doesn't scale; days → hours as the goal. Series kickoff. |
| **1** | "Self-service doesn't mean giving up the audit trail." | RFC-0001 | Why provisioning opens a PR instead of mutating directly — and how auto-merge removes the friction. |
| **2** | "If a team's request triggers a Terraform run, it's in the wrong layer." | ADR-0001 | The two-layer boundary, with a portable litmus test. |
| **3** | "The most valuable thing I built this month is something I didn't build." | ADR-0003 | Why no portal and no Crossplane in v1 — and the written revisit triggers. Contrarian, high-engagement. |
| **4** | "Never close the old path before the new one is faster." | RFC-0001 rollout | Platform adoption strategy; why restricting access early breeds resentment. |
| **5** | "An alert without a runbook is a bug, not noise." | Phase 4 / runbooks | On-call health as a design target, not a rota problem. |
| **6** | "Where I let AI near production — and where I don't." | ARCHITECTURE §7 | The boundary table: context assembly yes, unattended mutation no. Timely and differentiating. |

### Milestone-gated (ship when the demo works)

| # | Working title | Gate |
|---|---|---|
| **7** | "I wrote the controller instead of configuring one." | `environment-controller` reconciles a real `Environment` |
| **8** | "A new environment in 6 minutes. Self-served." | End-to-end Phase 2 flow, with a timing |
| **9** | "Adding a region touched one variable file." | ApplicationSet fan-out to a second region |
| **10** | "A bad deploy reached 5% of traffic. Then it reached none." | Argo Rollouts canary + automated rollback |
| **11** | "What an SLO changed about how we deploy." | Phase 4 SLOs + error budget in use |

---

## Mechanics

- **Cadence:** aim for one post every 1–2 weeks. Alternate decision/milestone so a slow build week doesn't break the streak.
- **Visual:** a cropped architecture diagram, the `Environment` YAML, terminal output, or a dashboard. One per post. (Reuse the SVG recipe in the sibling repo's `linkedin-posts/diagrams/`.)
- **The `Environment` spec is your best visual** — a small YAML block that shows a whole contract is instantly legible to platform engineers and invites "why is X a field but not Y?" comments.
- **First comment** carries links (LinkedIn suppresses in-post outbound links) and the series index.
- **Engagement:** end on genuine disagreement bait. Platform engineers *love* arguing about the Terraform boundary and about portals — posts 2 and 3 should draw the most comments.
- **Reply within 2 hours.** For a hiring-adjacent goal, the comment thread is where the actual conversations start.
