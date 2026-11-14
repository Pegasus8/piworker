import { ref } from 'vue'
import { msg } from '@/i18n'
// warning: the action succeeded, with something the user should know (it
// stays until dismissed, like an error).
export type Feedback = { kind: 'success' | 'warning' | 'error' | 'pending'; text: string }

// failure: the error text, or a function of the error (for example the
// server's message with a fallback).
export function useFeedback() {
  const feedback = ref<Feedback | null>(null)
  const pending = ref(false)
  async function run(action: () => Promise<unknown>, success: string, failure: string | ((e: unknown) => string)) {
    if (pending.value) return false
    pending.value = true
    feedback.value = { kind: 'pending', text: msg().common.working }
    try {
      await action()
      feedback.value = { kind: 'success', text: success }
      return true
    } catch (e) {
      feedback.value = { kind: 'error', text: typeof failure === 'function' ? failure(e) : failure }
      return false
    } finally { pending.value = false }
  }
  return { feedback, pending, run }
}
