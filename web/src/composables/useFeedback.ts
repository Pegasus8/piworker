import { ref } from 'vue'
export type Feedback = { kind: 'success' | 'error' | 'pending'; text: string }

export function useFeedback() {
  const feedback = ref<Feedback | null>(null)
  const pending = ref(false)
  async function run(action: () => Promise<unknown>, success: string, failure: string) {
    if (pending.value) return false
    pending.value = true
    feedback.value = { kind: 'pending', text: 'Working…' }
    try {
      await action()
      feedback.value = { kind: 'success', text: success }
      return true
    } catch {
      feedback.value = { kind: 'error', text: failure }
      return false
    } finally { pending.value = false }
  }
  return { feedback, pending, run }
}
