// The AI memory dialog's lists (docs/design/llm.md §8): pages of a
// collection's documents, the conversations a filter keeps, short previews,
// and where the focus goes when a row goes. Texts come from messages that
// reached the flows: the panels show them through AgentText (hidden
// characters escaped), never as HTML.
import { nextTick } from 'vue'
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

const SPACE = /\s/

/**
 * A text on one line (whitespace collapsed), cut at max characters. Reads
 * the text only up to the cut: a document can be a whole book.
 */
export function textPreview(text: string, max = 160): string {
  let line = ''
  let count = 0
  let space = false
  for (const char of text) {
    if (SPACE.test(char)) {
      space = count > 0
      continue
    }
    if (space) {
      if (count === max) return `${line}…`
      line += ' '
      count++
      space = false
    }
    if (count === max) return `${line}…`
    line += char
    count++
  }
  return line
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

/**
 * The row to put the focus on once the view of key is left, or key's row
 * is gone (cleared, deleted): its own row while the list shows it, else the
 * next row still listed, else the previous one; null when key is not
 * listed (filtered out) or none is left, for the list's own controls.
 * before and after: the keys listed before and after the change.
 */
export function focusRowAfter(key: string | null, before: readonly string[], after: readonly string[]): string | null {
  if (key === null) return null
  if (after.includes(key)) return key
  const i = before.indexOf(key)
  if (i < 0) return null
  const listed = (k: string) => after.includes(k)
  return before.slice(i + 1).find(listed) ?? before.slice(0, i).reverse().find(listed) ?? null
}

/** The element ID of a row's control (prefix-key), whatever the key holds: escaped, no spaces. */
export function rowControlId(prefix: string, key: string): string {
  return `${prefix}-${encodeURIComponent(key)}`
}

/**
 * The element IDs to focus, the first one present, once the view of key is
 * left or key's row is gone: the control (rowPrefix) of the row
 * focusRowAfter picks, then the list's own controls (fallbacks).
 */
export function focusCandidates(key: string | null, before: readonly string[], after: readonly string[], rowPrefix: string, fallbacks: readonly string[]): string[] {
  const row = focusRowAfter(key, before, after)
  return [...(row === null ? [] : [rowControlId(rowPrefix, row)]), ...fallbacks]
}

/**
 * Puts the focus back once the view of key is left or key's row is gone
 * (its control went with it): on the first of focusCandidates present
 * once the list is drawn. after reads the keys listed then.
 */
export async function restoreFocus(key: string | null, before: readonly string[], after: () => readonly string[], rowPrefix: string, fallbacks: readonly string[]): Promise<void> {
  await nextTick()
  for (const id of focusCandidates(key, before, after(), rowPrefix, fallbacks)) {
    const target = document.getElementById(id)
    if (target) {
      target.focus()
      return
    }
  }
}

/**
 * The memory key the empty list gives as an example. Telegram updates
 * reach a flow through the Webhook trigger, which puts the request body
 * under payload.body: a path without it renders empty, and every chat
 * would share the one conversation "telegram:".
 */
export const EXAMPLE_MEMORY_KEY = 'telegram:{{payload.body.message.chat.id}}'
