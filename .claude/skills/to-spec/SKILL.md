---
name: to-spec
description: Turn the current conversation into a spec and publish it to the project issue tracker. No interview, just synthesis of what you've already discussed.
disable-model-invocation: true
---

This skill takes the current conversation context and codebase understanding and produces a spec (you may know this document as a PRD). DON'T interview the user; just synthesize what you already know.

The issue tracker and triage label vocabulary should have been provided to you; if not, ask the user or default to the local-markdown tracker.

## Process

1. Explore the repo to understand the current state of the codebase, if you haven't already. Use the domain vocabulary from `llm-wiki/` throughout the spec (read `llm-wiki/index.md` first), and respect the conventions in the root `AGENTS.md` for the area you're touching.

2. Sketch out the seams at which you plan to test the feature. Existing seams should be preferred to new ones. Use the highest seam possible. If new seams are needed, propose them at the highest point you can. The fewer seams across the codebase, the better; the ideal number is one.

Check with the user that these seams match their expectations.

3. Write the spec using the template below, then publish it to the project issue tracker. Apply the `ready-for-agent` triage label; skip additional triage.

<spec-template>

## Problem Statement

The problem that the user is facing, from the user's perspective.

## Solution

The solution to the problem, from the user's perspective.

## User Stories

A LONG, numbered list of user stories. Each user story should be in the format of:

1. As an <actor>, I want a <feature>, so that <benefit>

<user-story-example>
1. As a mobile bank customer, I want to see balance on my accounts, so that I can make better informed decisions about my spending
</user-story-example>

This list of user stories should be extremely extensive and cover all aspects of the feature.

## Implementation Decisions

A list of implementation decisions that were made. This can include:

- The modules to be built/modified
- The interfaces of those modules to be modified
- Technical clarifications from the developer
- Architectural decisions
- Schema changes
- API contracts
- Specific interactions

DON'T include specific file paths or code snippets. They may end up being outdated very quickly.

Exception: a prototype snippet may encode a decision more precisely than prose can (state machine, reducer, schema, type shape). Inline it within the relevant decision and note briefly that it came from a prototype. Trim to the decision-rich parts: not a working demo, just the important bits.

## Testing Decisions

A list of testing decisions that were made. Include:

- A description of what makes a good test (only test external behavior, not implementation details)
- Which modules to test
- Prior art for the tests (that is, similar types of tests in the codebase)

## Out of Scope

A description of the things that are out of scope for this spec.

## Further Notes

Any further notes about the feature.

</spec-template>
