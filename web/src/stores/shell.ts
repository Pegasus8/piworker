import { defineStore } from 'pinia'
import { ref } from 'vue'

// What the navigation opens over any page: the global settings dialogs and,
// on small screens, the navigation drawer.
export const useShellStore = defineStore('shell', () => {
  const showVariables = ref(false)
  const showSecrets = ref(false)
  const showBrokers = ref(false)
  const drawerOpen = ref(false)
  return { showVariables, showSecrets, showBrokers, drawerOpen }
})
