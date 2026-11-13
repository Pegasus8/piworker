import { defineStore } from 'pinia'
import { ref } from 'vue'

// What the navigation opens over any page: the global settings dialogs and,
// on small screens, the navigation drawer.
export const useShellStore = defineStore('shell', () => {
  const showVariables = ref(false)
  const showSecrets = ref(false)
  const showBrokers = ref(false)
  const showBackup = ref(false)
  const drawerOpen = ref(false)
  // Bumped when the secrets dialog changed a secret, for pages that show them.
  const secretsChanged = ref(0)
  const brokersChanged = ref(0)
  return { showVariables, showSecrets, showBrokers, showBackup, drawerOpen, secretsChanged, brokersChanged }
})
