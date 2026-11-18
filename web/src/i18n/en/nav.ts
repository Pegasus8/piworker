export default {
  main: 'Main navigation',
  openMenu: 'Open navigation',
  closeMenu: 'Close navigation',
  flows: 'Flows',
  devices: 'Zigbee devices',
  agents: 'Agents',
  agentsWaiting: (requests: number, drafts: number) => {
    const parts: string[] = []
    if (requests) parts.push(`${requests} request${requests === 1 ? '' : 's'} waiting for approval`)
    if (drafts) parts.push(`${drafts} draft${drafts === 1 ? '' : 's'} to review`)
    return parts.length ? `Agents: ${parts.join(', ')}` : 'Agents'
  },
  settings: 'Settings',
  variables: 'Variables',
  variablesHint: 'Global variables every flow can read',
  secrets: 'Secrets',
  secretsHint: 'Passwords and tokens nodes use without showing them',
  brokers: 'Broker connections',
  brokersHint: 'MQTT brokers for MQTT and Zigbee nodes',
  backup: 'Export and restore',
  backupHint: 'Variables, connections and secrets in one file',
  backups: 'Backups',
  https: 'HTTPS and this device',
  account: 'Account',
  logout: 'Sign out',
  logoutFailed: 'Could not sign out. Check your connection and try again.',
}
