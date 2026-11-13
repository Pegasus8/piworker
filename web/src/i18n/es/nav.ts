import type en from '../en/nav'
const es: typeof en = {
  main: 'Navegación principal',
  openMenu: 'Abrir la navegación',
  closeMenu: 'Cerrar la navegación',
  flows: 'Flujos',
  devices: 'Dispositivos Zigbee',
  agents: 'Agentes',
  agentsWaiting: (requests: number, drafts: number) => {
    const parts: string[] = []
    if (requests) parts.push(`${requests} ${requests === 1 ? 'solicitud espera' : 'solicitudes esperan'} aprobación`)
    if (drafts) parts.push(`${drafts} ${drafts === 1 ? 'borrador' : 'borradores'} por revisar`)
    return parts.length ? `Agentes: ${parts.join(', ')}` : 'Agentes'
  },
  settings: 'Ajustes',
  variables: 'Variables',
  variablesHint: 'Variables globales que cualquier flujo puede leer',
  secrets: 'Secretos',
  secretsHint: 'Contraseñas y tokens que los nodos usan sin mostrarlos',
  brokers: 'Conexiones a brokers',
  brokersHint: 'Brokers MQTT para los nodos MQTT y Zigbee',
  backup: 'Exportar y restaurar',
  backupHint: 'Variables, conexiones y secretos en un archivo',
  backups: 'Copias de seguridad',
  https: 'HTTPS y este dispositivo',
  account: 'Cuenta',
  logout: 'Cerrar sesión',
  logoutFailed: 'No se pudo cerrar la sesión. Revisa tu conexión e inténtalo de nuevo.',
}
export default es
