# Postmortem: <short title>

**Date:** YYYY-MM-DD · **Duration:** Xh Ym · **Author:** · **Status:** draft | reviewed

> Blameless. The goal is to fix the system that let a reasonable person cause this, not to find who typed
> the command. If a person could do the wrong thing easily, that's a platform defect.

## Impact

Who was affected, for how long, and in terms they'd recognize ("14 teams couldn't provision environments
for 90 minutes"), not internal ones ("the reconciler backed off").

## Timeline

| Time (UTC) | Event |
|---|---|
| 00:00 | Change deployed / trigger |
| 00:00 | First signal (alert? or a human noticing — note which) |
| 00:00 | Mitigation started |
| 00:00 | Resolved |

## What happened

The technical narrative. Cause, contributing factors, and why it wasn't caught earlier.

## Detection

How did we find out? **If a human noticed before an alert did, that's a finding** — capture it as an action.

## What went well

Genuinely — fast rollback, good runbook, clear comms. Worth recording so it survives.

## What was hard

Missing context, confusing dashboards, unclear ownership, a runbook that didn't match reality.

## Action items

Prevention over detection; detection over mitigation. Each item needs an owner and a date, and gets tracked
like any other work — unowned action items are how the same incident happens twice.

| Action | Type | Owner | Due |
|---|---|---|---|
| | prevention / detection / mitigation / docs | | |

## Guardrail check

The platform-specific question: **could a guardrail have made this impossible rather than merely
detectable?** If yes, that's the highest-value action item on the list.
