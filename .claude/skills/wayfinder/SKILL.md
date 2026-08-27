---
name: wayfinder
description: Plan a huge chunk of work (more than one agent session can hold) as a shared map of investigation tickets on your issue tracker, and resolve them one at a time until the way to the destination is clear.
disable-model-invocation: true
---

A loose idea has arrived, too big for one agent session and wrapped in fog. The way from here to the **destination** isn't visible yet. Wayfinding is about finding that way, not charging at the destination. This skill charts the way as a **shared map** on the repo's issue tracker. It then works the tickets one at a time until the route is clear.

The destination varies per effort, and naming it is the first act of charting; it shapes every ticket. It might be a spec to hand off and iterate on, or a decision to lock before planning starts. It might even be a change made in place, like a data-structure migration. The map is domain-agnostic: engineering work, course content, whatever fits the shape.

## Plan, don't do

Wayfinder is **planning** by default: each ticket resolves a decision. The map is done when the way is clear: nothing left to decide before someone goes and does the thing. The pull to go do the work is usually the signal. You've reached the edge of the map, and it's time to hand off. An effort can override this in its **Notes** (carrying execution into the map itself), but absent that, produce decisions, not deliverables.

## Refer by name

Every map and ticket is an issue, so it has a **name**: its title. In everything the human reads (narration, the map's Decisions-so-far), refer to it by that name. Never use a bare id, number, or slug. A wall of `#42, #43, #44` is illegible; names read at a glance. The id and URL don't vanish (a name wraps its link), but they ride *inside* the name, never stand in for it.

## The Map

The map is a single issue on this repo's issue tracker, labelled `wayfinder:map` (the canonical artifact). Its tickets are child issues of the map.

The map is an **index**, not a store. It lists the decisions made and points at the tickets that hold their detail. A decision lives in exactly one place (its ticket), so the map never restates it, only gists it and links.

**Where the map, its child tickets, blocking, and frontier queries physically live is tracker-specific.** The issue tracker should have been provided to you; if not, ask the user or default to the local-markdown tracker.

### The map body

The whole map at low resolution, loaded once per session. Open tickets **aren't** listed; they are open child issues, found by query.

```markdown
## Destination

<what reaching the end of this map looks like: the spec, decision, or change this effort is finding its way to. One or two lines; every session orients to it before choosing a ticket.>

## Notes

<domain; skills every session should consult; standing preferences for this effort>

## Decisions so far

<!-- the index; one line per closed ticket: enough to judge relevance, then zoom the link for the detail the ticket holds -->

- [<closed ticket title>](link): <one-line gist of the answer>

## Not yet specified

<!-- see "Fog of war": in-scope fog you can't ticket yet; graduates as the frontier advances -->

## Out of scope

<!-- see "Out of scope": work ruled beyond the destination; closed, never graduates -->
```

### Tickets

Each ticket is a **child issue** of the map; the tracker's issue id is its identity. Its body is the question, sized to one 100K token agent session:

```markdown
## Question

<the decision or investigation this ticket resolves>
```

Each ticket carries a `wayfinder:<type>` label, one of `research`, `prototype`, `grilling`, `task` (see [Ticket Types](#ticket-types)).

A session **claims** a ticket by assigning it to the dev driving the map, **first**, before any work, so concurrent sessions skip it. That assignee _is_ the claim: an open, unassigned ticket is unclaimed.

Blocking uses the tracker's **native** dependency relationship. Native blocking matters: it renders the frontier _visually_ in the tracker's own UI. The human sees what's takeable without opening the map. Only a tracker that lacks native blocking falls back to a body convention. A ticket is **unblocked** when every ticket blocking it is closed; the **frontier** is the open, unblocked, unclaimed children: the edge of the known.

The answer isn't part of the body; it's recorded on resolution (see [Work through the map](#work-through-the-map)). Assets created while resolving a ticket are linked from the issue, not pasted in.

## Ticket Types

Every ticket is either **HITL** (human in the loop, worked *with* a human who speaks for themselves) or **AFK**, driven by the agent alone. A HITL ticket only resolves through that live exchange. The agent never stands in for the human's side of it (a grilling agent that answers its own questions has broken this).

- **Research** (AFK): Reading documentation, third-party APIs, or local resources like knowledge bases. Creates a markdown summary as a linked asset. Use when knowledge outside the current working directory is required.
- **Prototype** (HITL): Raise the discussion's fidelity with a cheap, rough artifact, such as an outline, stub, or UI code. Link the prototype as an asset. Use it when appearance or behavior is the key question.
- **Grilling** (HITL): Conversation via the /grilling skill, one question at a time. The default case.
- **Task** (HITL or AFK): Manual work required before a *decision* can be made. Examples include provisioning access or moving data for inspection. A task acts instead of deciding, and it must unblock a decision rather than deliver the destination. The agent works alone where possible (AFK). Otherwise, it gives the human a precise checklist (HITL). Resolve it when the work finishes. Record the completed work and any facts that later tickets need.

## Fog of war

The map is _deliberately_ incomplete: don't chart what you can't yet see. Beyond the live tickets lies the **fog of war**. It's the dim view of decisions and investigations you can tell are coming but can't yet pin down. They hang on questions still open. Resolving a ticket clears the fog ahead of it, graduating whatever's now specifiable into fresh tickets (one at a time). The map is finished when the way to the destination is clear and no tickets remain.

The map's **Not yet specified** section records that dim view: the suspected question and area to revisit. It is the undiscovered frontier _toward_ the destination. Everything here is in scope but not sharp enough to ticket. Write as loosely or fully as the view allows. It also guides collaborators toward the effort's direction.

**Fog or ticket?** The test is whether you can state the question precisely now, _not_ whether you can answer it now.

- **Ticket when** the question is already sharp, even if it's blocked and you can't act on it yet.
- **Not yet specified when** you can't yet phrase it that sharply. Don't pre-slice the fog into ticket-sized pieces. It is coarser than a ticket. One patch may become several tickets, or none, when the frontier reaches it.

**Not yet specified** excludes what's already decided (Decisions so far) and what's already a live ticket. It also excludes what's out of scope (the next section).

## Out of scope

Fog only ever gathers _toward_ the destination. The destination fixes the scope, so work beyond it is **out of scope**. It isn't fog, and it doesn't belong in **Not yet specified**. It gets its own **Out of scope** section on the map: work you've consciously ruled out of _this_ effort. Scope, not sharpness, lands it here.

Out-of-scope work never graduates; the frontier stops at the destination. It returns only if the destination is redrawn, and then as a fresh effort, not a resumption.

Ruling something out of scope is a scoping act, not a step on the route. Sometimes a ticket that already exists turns out to sit past the destination (mis-scoped in while charting, or exposed by a resolution). **Close it**: a closed ticket is unambiguously off the frontier. Then leave one line in the **Out of scope** section: the gist plus why it's out of scope, linking the closed ticket. It stays out of **Decisions so far**, which records the route actually walked; a scope boundary isn't a step on it.

## Invocation

Two modes. Either way, **never resolve more than one ticket per session.**

### Chart the map

User invokes with a loose idea.

1. **Name the destination.** Run a `/grilling` session to identify the spec, decision, or change that this map seeks. The destination fixes the scope, so settle it first.
2. **Map the frontier.** Grill again, this time **breadth-first**. Fan out across the space to surface open decisions and immediately available steps. **If this surfaces no fog**, you don't need a map. Stop and ask the user how they'd like to proceed.
3. **Create the map** with the `wayfinder:map` label. Fill Destination and Notes, leave Decisions-so-far empty, and sketch the fog under **Not yet specified**.
4. **Create the tickets you can specify now** as child issues of the map. Wire blocking edges in a **second pass**, after issues have ids. Wiring separates the frontier from blocked work. Everything you can't specify stays in the fog under **Not yet specified**.
5. Stop: charting the map is one session's work; don't also resolve tickets.

### Work through the map

User invokes with a map (URL or number). A ticket is **optional**: without one, you pick the next decision, not the user.

1. Load the **map**: the low-res view, not every ticket body.
2. Choose the ticket. If the user named one, use it. Otherwise take the first frontier ticket in order. **Claim it**: assign it to yourself before any work.
3. Resolve it and **zoom as needed**: fetch the full body of any related or closed ticket on demand; invoke the skills the `## Notes` block names. If in doubt, use `/grilling`.
4. Record the resolution: post the answer as a **resolution comment**, **close** the issue, and **append a context pointer** to the map's Decisions-so-far.
5. Add newly surfaced tickets, then wire them. Graduate any fog that the answer makes specifiable. Remove each graduated item from **Not yet specified** so only its ticket remains. If any ticket sits beyond the destination, **rule it out of scope**. Update or delete tickets invalidated by the decision.

The user may run unblocked tickets in parallel, so expect other sessions to be editing the tracker concurrently.
