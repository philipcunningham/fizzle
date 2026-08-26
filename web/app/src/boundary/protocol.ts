// The browser protocol's method names and wire metadata have one checked-in
// source shared with the Go parity test. Payload conversion stays in the
// worker because it is an adapter concern, not a domain schema.
import type { Core } from "./contract";
import { coreMethods } from "./protocol.generated";

export type CoreMethod = (typeof coreMethods)[number];

type Equal<A, B> = [A] extends [B] ? ([B] extends [A] ? true : false) : false;
export type Assert<T extends true> = T;
export type ExactProtocol<T> = Equal<CoreMethod, keyof T>;

// A Core method added without a manifest entry, or a stale manifest name,
// fails type checking before either side of the worker can drift.
export type ProtocolMatchesCore = Assert<ExactProtocol<Core>>;
