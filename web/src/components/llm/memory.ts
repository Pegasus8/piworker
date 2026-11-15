// The AI memory dialog's lists (docs/design/llm.md §8): pages of a
// collection's documents, the conversations a filter keeps, and short
// previews. Texts come from messages that reached the flows: the panels
// show them through AgentText (hidden characters escaped), never as HTML.
import type { LLMConversationInfo } from '@/types'

/** Documents per page (GET /api/llm/knowledge/{collection} answers at most 100). */
export const DOCUMENT_PAGE = 50

export interface PageInfo {
  /** 1-based position of the first and last document shown (0 when none). */
  from: number
  to: number
  total: number
  hasPrev: boolean
  hasNext: boolean
}

/** Where a page of count documents starting at offset is, out of total. */
export function pageInfo(offset: number, count: number, total: number): PageInfo {
  return {
    from: count ? offset + 1 : 0,
    to: offset + count,
    total,
    hasPrev: offset > 0,
    hasNext: offset + count < total
  }
}

/** The page to show once a delete left total documents: the previous page when this one became empty. */
export function offsetAfterDelete(offset: number, total: number, pageSize = DOCUMENT_PAGE): number {
  return offset > 0 && offset >= total ? Math.max(0, offset - pageSize) : offset
}

/** Conversations listed at once (memory keeps up to 5 000): a filter narrows the rest. */
export const CONVERSATION_ROWS = 200

/** The conversations whose key holds query (any case), at most limit, and how many more match. */
export function filterConversations(list: readonly LLMConversationInfo[], query: string, limit = CONVERSATION_ROWS): { items: LLMConversationInfo[]; more: number } {
  const q = query.trim().toLowerCase()
  const matches = q ? list.filter(c => c.key.toLowerCase().includes(q)) : [...list]
  return { items: matches.slice(0, limit), more: Math.max(0, matches.length - limit) }
}

/** A text on one line (whitespace collapsed), cut at max characters. */
export function textPreview(text: string, max = 160): string {
  const line = text.replace(/\s+/g, ' ').trim()
  const chars = [...line]
  return chars.length > max ? `${chars.slice(0, max).join('')}…` : line
}

/** A document's metadata as compact JSON ('' when it has none). */
export function metadataText(metadata?: Record<string, unknown>): string {
  if (!metadata || !Object.keys(metadata).length) return ''
  try {
    return JSON.stringify(metadata)
  } catch {
    return ''
  }
}
