// Compile-time checks that the hand-written types of index.ts still match
// what the server sends (wire.gen.ts, generated from its Go structs). There
// is no runtime code here: vue-tsc fails on a line below when a Go response
// struct gains, loses or renames a field, or changes whether one is
// optional, and the hand-written type was not updated with it.
import type * as wire from './wire.gen'
import type {
  AssistantFlow,
  AssistantFlowResult,
  CallResult,
  DeploymentMark,
  FlowEvent,
  FlowRun,
  KnowledgeCollection,
  KnowledgeDocument,
  KnowledgeDocumentPage,
  LLMConnection,
  LLMConnectionTest,
  LLMConversation,
  LLMConversationInfo,
  LLMModel,
  LLMTurn,
  LLMUsage,
  NodeEventRecord,
  StoredFlowConnection,
  StoredFlowNode,
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
  // LLM connections, memory, knowledge base and the flow assistant (docs/design/llm.md §6).
  llmConnection: Matches<LLMConnection, wire.LLMConnectionItem>
  llmUsage: Matches<LLMUsage, wire.Usage>
  llmModel: Matches<LLMModel, wire.ModelInfo>
  llmConnectionTest: Matches<LLMConnectionTest, wire.LLMConnectionTest>
  llmConversationInfo: Matches<LLMConversationInfo, wire.ConversationInfo>
  llmTurn: Matches<LLMTurn, wire.Turn>
  llmConversation: Matches<LLMConversation, wire.LLMConversation>
  knowledgeCollection: Matches<KnowledgeCollection, wire.CollectionInfo>
  knowledgeDocument: Matches<KnowledgeDocument, wire.Document>
  knowledgeDocumentPage: Matches<KnowledgeDocumentPage, wire.KnowledgeDocumentPage>
  assistantFlowResult: Matches<AssistantFlowResult, wire.AssistantFlowResult>
  assistantFlow: Matches<AssistantFlow, wire.AssistantFlow>
  storedFlowNode: Matches<StoredFlowNode, wire.Node>
  storedFlowConnection: Matches<StoredFlowConnection, wire.Connection>
} = {
  flowRun: true,
  nodeEventRecord: true,
  flowEvent: true,
  deploymentMark: true,
  versionEntry: true,
  versionList: true,
  callResult: true,
  tlsInfo: true,
  llmConnection: true,
  llmUsage: true,
  llmModel: true,
  llmConnectionTest: true,
  llmConversationInfo: true,
  llmTurn: true,
  llmConversation: true,
  knowledgeCollection: true,
  knowledgeDocument: true,
  knowledgeDocumentPage: true,
  assistantFlowResult: true,
  assistantFlow: true,
  storedFlowNode: true,
  storedFlowConnection: true,
}
