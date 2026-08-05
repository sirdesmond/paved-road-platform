# Post 3 — What I deliberately didn't build

**Status:** ready to publish (from ADR-0003)
**Source:** `docs/adr/0003-no-portal-or-crossplane-in-v1.md`
**Visual:** none needed, this one reads fine as text.

---

## Final copy

Two things I've left out of this platform that people keep asking about: a developer portal and Crossplane.

Not because there's anything wrong with either. I just couldn't justify them yet.

The portal was the harder one to skip, because it's what everyone pictures when you say self-service. But the problem I'm trying to fix is that provisioning takes days. Putting a nice interface in front of a slow process gets you a slow process with a nice interface. There's also a subtler thing: if you build the UI first, you end up shaping the whole contract around a screen, when the part that has to last is the API underneath it.

Crossplane I thought about for a lot longer, and I do use it on another project. It's genuinely good. But creating an environment here is mostly a sequence of steps. Check the request, check there's room, generate the files, open a PR, record that it happened. That's ordinary code and I'd rather keep it somewhere I can write tests against it. Running two reconcilers next to each other also gives odd failures twice as many places to hide.

The thing I made myself do was write down what would change my mind, because otherwise "not yet" just quietly turns into "never".

For the portal: when people start asking what exists and who owns it often enough that answering becomes somebody's job. That's a different problem and a portal is the right answer to it.

For Crossplane: the moment teams want databases and queues alongside their environments. I'm not going to write a worse version of that myself.

Anyone else deliberately left out the tool everyone assumed you'd use?

#PlatformEngineering #Kubernetes #Backstage #Crossplane #SoftwareArchitecture

—
I'm building an internal developer platform in the open. The write-ups and decisions are in the repo.

---

## First comment

> Part 3 of "Paved Roads". Full write-up in the repo. I do run Crossplane on another project, so this is about scope here rather than a dig at the tool. Part 2 here: [link].

---

## Notes

- People save posts about what someone chose not to do, because almost nobody writes them.
- The "not yet turns into never" line is the takeaway. It's plainer than the version I had before and lands better for it.
- First comment heads off the "sounds like you've never used Crossplane" reply. Worth keeping.
- Tagging Backstage and Crossplane puts it in front of the people most likely to argue, which is what you want.
