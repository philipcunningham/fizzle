# Architecture

One validated document owns its bytes and resolved layout. Adapters never infer bank, voice, or audio boundaries independently.

```text
CLI adapters          WASM protocol          React workflows
     |                      |                       |
diskfs storage       webcore Session          view state
              \            |            /
                document.State
                      |
                 fzf.Document
                      |
            patches, bytes, and codecs
```

## Ownership

`pkg/fzf` owns full-dump construction, resolved layouts, bounded views, voice-count authority, and byte-preserving operations.

`pkg/document` owns atomic operations across one or two disk images. A failed operation leaves every input unchanged.

`pkg/diskfs` owns in-memory directory allocation and file operations. Adapters own paths, locking, rendering, and logging.

`pkg/webcore` owns browser sessions, revisions, history, schemas, projections, and structured errors. It publishes state only after parsing and projection succeed.

React owns view state and workflows. Binary parsing, mutation, and capacity rules remain in Go.

## Enforced boundaries

Architecture tests enforce dependency boundaries, projection parsing rules, allowed layout APIs, and the session facade's size.

Protocol tests compare Go registration, worker dispatch, and the TypeScript contract. CLI tests pin generated reference documentation to executable command metadata.

## Fixtures

Tracked synthetic and hardware corpus fixtures support offline tests. Corpus tests run against the real sampler files under `testdata/corpus`.
