// Compile-time checks that the hand-written types of index.ts still match
// what the server sends (wire.gen.ts, generated from its Go structs). There
// is no runtime code here: vue-tsc fails on a line below when a Go response
// struct gains, loses or renames a field, or changes whether one is
// optional, and the hand-written type was not updated with it.
import type * as wire from './wire.gen'
import type {
  CallResult,
  DeploymentMark,
  FlowEvent,
  FlowRun,
  NodeEventRecord,
  TLSInfo,
  VersionEntry,
  VersionList,
} from './index'

type OptionalKeys<T> = { [K in keyof T]-?: object extends Pick<T, K> ? K : never }[keyof T]
type Same<A, B> = [A] extends [B] ? ([B] extends [A] ? true : false) : false

/**
 * True when Hand has exactly Wire's fields, the same ones optional, and
 * each field's type is one the wire type allows (a union of literals for a
 * string, for example).
 */
type Matches<Hand, Wire> = Same<keyof Hand, keyof Wire> extends true
  ? Same<OptionalKeys<Hand>, OptionalKeys<Wire>> extends true
    ? [Required<Hand>] extends [Required<Wire>]
      ? true
      : 'a field has a type the server does not send'
    : 'a field is optional on one side only'
  : 'the fields differ'

export const wireChecks: {
  flowRun: Matches<FlowRun, wire.FlowRun>
  nodeEventRecord: Matches<NodeEventRecord, wire.NodeEventRecord>
  flowEvent: Matches<FlowEvent, wire.Event>
  deploymentMark: Matches<DeploymentMark, wire.DeploymentView>
  versionEntry: Matches<VersionEntry, wire.VersionEntry>
  versionList: Matches<VersionList, wire.VersionList>
  callResult: Matches<CallResult, wire.CallResult>
  tlsInfo: Matches<TLSInfo, wire.TLSInfo>
} = {
  flowRun: true,
  nodeEventRecord: true,
  flowEvent: true,
  deploymentMark: true,
  versionEntry: true,
  versionList: true,
  callResult: true,
  tlsInfo: true,
}
