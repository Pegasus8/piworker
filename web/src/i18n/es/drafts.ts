import type en from '../en/drafts'

type Change = 'added' | 'removed' | 'changed'

const count = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`
const nodeChange: Record<Change, [string, string]> = {
  added: ['añadido', 'añadidos'],
  removed: ['eliminado', 'eliminados'],
  changed: ['modificado', 'modificados']
}

const es: typeof en = {
  alertKinds: {
    risk_raised: {
      label: 'Riesgo mayor',
      description: 'El flujo en conjunto puede hacer más que antes: su nivel de riesgo es más alto.'
    },
    privileged_node_added: {
      label: 'Nodo privilegiado',
      description: 'Ahora se ejecuta un nodo que puede ejecutar comandos, escribir archivos, consultar bases de datos o acceder al hardware.'
    },
    destination_changed: {
      label: 'Destino cambiado',
      description: 'Cambió adónde envía datos o se conecta un nodo: una URL, host, broker, tema, destinatario, chat o ruta de archivo.'
    },
    dynamic_destination: {
      label: 'Destino dinámico',
      description: 'Un destino se construye con datos del mensaje o variables, así que adónde van los datos puede cambiar mientras el flujo está en marcha.'
    },
    new_host: {
      label: 'Host nuevo',
      description: 'El flujo contacta un host que la versión comparada nunca contacta.'
    },
    secret_added: {
      label: 'Secreto nuevo en uso',
      description: 'El flujo usa un secreto que la versión comparada nunca usa.'
    },
    secret_moved: {
      label: 'Secreto usado en otro lugar',
      description: 'Un secreto lo usa un nodo o ajuste que antes no lo usaba, así que podría enviarse a un sitio nuevo.'
    },
    risk_switch_on: {
      label: 'Opción de riesgo activada',
      description: 'Está activada una opción que amplía lo que puede hacer un nodo (como el acceso a la red privada o los argumentos con guion).'
    },
    sensitive_literal: {
      label: 'Credencial escrita',
      description: 'Un ajuste de credencial tiene un valor escrito en lugar de una referencia {{secret.NAME}}.'
    },
    credentialed_request_changed: {
      label: 'Credencial usada de otra forma',
      description: 'Un nodo que usa una credencial ahora envía otra solicitud o recibe una entrada nueva, así que la credencial que aprobaste se usa para otra cosa.'
    },
    rate_changed: {
      label: 'Frecuencia cambiada',
      description: 'En un flujo que llega a otros servicios, cambió la frecuencia con que se activa un disparador o cuántos mensajes deja pasar un limitador.'
    },
    global_variable_write: {
      label: 'Variable global escrita',
      description: 'Puede haber cambiado lo que el flujo escribe en una variable global. Otros flujos leen variables globales para sus URL, comandos y condiciones.'
    },
    global_variable_read: {
      label: 'Variable global leída por una herramienta',
      description: 'Una herramienta cuyo recorrido cambió lee variables globales: lo que lee vuelve al agente que la llama.'
    },
    tool_path_changed: {
      label: 'Herramienta cambiada',
      description: 'Cambió lo que ejecuta una herramienta publicada: un agente que la llame, con su permiso y su ajuste de confirmación, ejecutaría algo que no aprobaste.'
    },
    data_read_changed: {
      label: 'Lectura de datos protegidos',
      description: 'Un nodo nuevo o modificado lee variables, los ajustes que pasan nodos anteriores o lo que devolvió una credencial: podría devolvérselos a un agente a través de las ejecuciones del flujo.'
    }
  },
  unknownAlert: 'Alerta',
  unknownAlertDescription: 'Una alerta que este editor aún no conoce: lee su mensaje.',
  nodeLabel: (id: string) => `Nodo ${id}`,
  wholeFlow: 'Todo el flujo',
  onSetting: (node: string, setting: string) => `${node} · ajuste ${setting}`,

  risk: {
    safe: 'Seguro',
    external: 'Externo',
    privileged: 'Privilegiado',
    unknown: 'Riesgo desconocido',
    raised: (before: string, after: string) => `El riesgo sube de ${before} a ${after}`,
    lowered: (before: string, after: string) => `El riesgo baja de ${before} a ${after}`,
    same: (risk: string) => `Riesgo sin cambios: ${risk}`
  },

  groups: { added: 'Nodos añadidos', removed: 'Nodos eliminados', changed: 'Nodos modificados' },
  nodesChanged: (n: number, change: Change) => `${count(n, 'nodo', 'nodos')} ${nodeChange[change][n === 1 ? 0 : 1]}`,
  connectionsAdded: (n: number) => count(n, 'conexión añadida', 'conexiones añadidas'),
  connectionsRemoved: (n: number) => count(n, 'conexión eliminada', 'conexiones eliminadas'),
  noChanges: 'Sin cambios en nodos ni conexiones',
  fieldEnabled: 'Activado',
  fieldType: 'Tipo de nodo',
  hidden: 'oculto',
  fieldName: 'Nombre',
  fieldDescription: 'Descripción',

  errors: (n: number) => count(n, 'error', 'errores'),
  warnings: (n: number) => count(n, 'advertencia', 'advertencias'),
  noIssues: 'Sin problemas',
  severityError: 'Error',
  severityWarning: 'Advertencia',

  newFlowTitle: (name: string) => `Flujo nuevo «${name}»`,
  flowTitle: (id: string) => `Flujo ${id}`,

  flowNowAt: (version: number) => `el flujo está ahora en la versión ${version}`,
  flowDeleted: 'el flujo se eliminó',
  createsNewFlow: 'Crea un flujo nuevo',
  writtenFor: (version: number) => `Escrito para la versión ${version}`,
  changesCurrent: (version: number) => `Cambia la versión ${version} (la actual)`,
  writtenAsNew: 'Escrito como flujo nuevo',
  savedAsVersion: (version: number) => `guardado como versión ${version}`,

  noAlerts: 'Sin alertas: no hay cambios de destino, secretos, credenciales ni riesgo.',
  emptyHeading: 'Cambios respecto a un flujo vacío',
  emptyNoteNew: 'Comparado con un flujo vacío: este borrador crea un flujo nuevo.',
  emptyNoteDeleted: 'Comparado con un flujo vacío: el flujo para el que se escribió este borrador ya no existe.',
  notBase: (version: number) => `, no con la versión ${version} para la que se escribió el borrador`,
  pendingHeading: (current: number) => `Cambios respecto a la versión ${current} (actual)`,
  pendingNote: (current: number, notBase: string) => `Comparado con la versión actual, ${current}${notBase}.`,
  decidedHeading: (current: number) => `Cambios respecto a la versión ${current} (actual, recalculados ahora)`,
  savedAs: (version: number) => `Este borrador se guardó como versión ${version}`,
  wasApproved: 'Este borrador se aprobó',
  partOfIt: 'así que sus cambios ya forman parte de ella y aquí no aparece ninguno',
  partOfFlow: 'así que sus cambios ya forman parte del flujo',
  approvedNote: (current: number, notBase: string, saved: string, part: string) =>
    `Recalculado ahora respecto a la versión actual, ${current}${notBase}. ${saved}, ${part}: aquí solo aparece lo que difiere de la versión actual.`,
  noAlertsApplied: (current: number) => `Sin alertas respecto a la versión ${current}: los cambios de este borrador ya forman parte de ella.`,
  noAlertsCurrent: (current: number) => `Sin alertas respecto a la versión actual, ${current}.`,
  otherNote: (current: number, notBase: string) =>
    `Recalculado ahora respecto a la versión actual, ${current}${notBase}. No se guardó nada de este borrador.`,

  state: {
    outOfDate: 'Desactualizado',
    outOfDateExplanation: (base: number, current: number) =>
      `El agente escribió este borrador para la versión ${base}, pero el flujo está ahora en la versión ${current}. ` +
      'No se puede aprobar: pide al agente que lo rehaga sobre la versión actual, o recházalo.',
    waiting: 'Pendiente de revisión',
    waitingExplanation: 'Nada cambia hasta que lo apruebes.',
    deployedByAgent: 'Desplegado por un agente',
    deployedByAgentExplanation: (token: string, version?: number) =>
      `El agente ${token} desplegó este borrador por su cuenta${version ? ` como versión ${version}` : ''} (el nivel de despliegue de agentes lo permite); no lo revisaste. ` +
      'El historial de versiones puede restaurar la versión que aprobaste por última vez.',
    approved: 'Aprobado',
    approvedExplanation: (version?: number) =>
      version ? `Aprobaste este borrador; se guardó como versión ${version}.` : 'Aprobaste este borrador.',
    rejected: 'Rechazado',
    rejectedExplanation: 'Rechazaste este borrador. El agente ve el motivo.',
    void: 'Anulado',
    voidExplanation: 'Este borrador se cerró sin decisión (se revocó su token o se eliminó su flujo). No se guardó nada.',
    unknownExplanation: 'Este borrador ya no se puede aprobar.'
  },

  statusWords: { approved: 'aprobado', rejected: 'rechazado', decided: 'decidido' },
  blockDeployedByAgent: 'Su agente ya desplegó este borrador; no hay nada que aprobar.',
  blockVoid: 'Este borrador está anulado: se cerró sin decisión, así que no hay nada que aprobar.',
  blockDecided: (word: string) => `Este borrador ya fue ${word}; no hay nada que aprobar.`,
  blockStale: (current: number) =>
    `Este borrador está desactualizado: el flujo está ahora en la versión ${current}. Pide al agente que lo rehaga, o recházalo.`,
  blockErrors: (n: number) =>
    `El borrador tiene ${count(n, 'error', 'errores')} (en Problemas), así que no se puede guardar. Pide al agente que los corrija, o recházalo.`,
  blockSecretsRunning: 'El flujo está en marcha, así que aprobarlo lo vuelve a desplegar, pero usa secretos que no están definidos (en Problemas). Defínelos antes en Secretos.',
  blockSecretsDeploy: 'Para desplegar hacen falta secretos que no están definidos (en Problemas). Defínelos antes en Secretos, o aprueba sin desplegar.',

  alertsList: (n: number, kinds: string) => `${count(n, 'alerta', 'alertas')}: ${kinds}`,
  noAlertsLower: 'sin alertas',
  comparedBoth: (current: number, risk: string, alerts: string, last: number, sinceRisk: string, sinceAlerts: string) =>
    `Respecto a la versión actual, ${current}: ${risk}; ${alerts}. ` +
    `Respecto a la versión ${last}, la que aprobaste por última vez: ${sinceRisk}; ${sinceAlerts}. ` +
    'Al aprobar también apruebas esos cambios anteriores del agente.',
  createdFrom: (name: string, token: string) => `«${name}» se crea como flujo nuevo a partir de este borrador de «${token}».`,
  savedFrom: (name: string, token: string) => `«${name}» se guarda como versión nueva a partir de este borrador de «${token}».`,
  renames: (before: string, after: string) => ` Cambia el nombre del flujo de «${before}» a «${after}».`,
  describes: ' Cambia la descripción del flujo.',
  runsRedeploy: 'El flujo está en marcha, así que se vuelve a desplegar con esta versión.',
  runsDeploy: 'Se despliega de inmediato.',
  runsNot: 'No se despliega.',

  outcomeDeployError: (action: string, version: number, message: string) =>
    `${action}: guardado como versión ${version}, pero no se pudo desplegar: ${message}`,
  outcomeDeployed: (action: string, version: number) => `${action}: guardado como versión ${version} y desplegado.`,
  outcomeSaved: (action: string, version: number) => `${action}: guardado como versión ${version}. No está en marcha.`,

  marks: {
    deployedBy: (running: boolean | undefined, name: string) =>
      `${running ? 'Desplegado' : 'Desplegado por última vez'} por ${name ? `el agente ${name}` : 'un agente'}`,
    deployedTitle: (version: number, name: string, when: string, now?: number) =>
      `La versión ${version} la desplegó ${name ? `el token de agente «${name}»` : 'un token de agente'}${when ? ` el ${when}` : ''}.` +
      (now ? ` El flujo está ahora en la versión ${now}.` : ''),
    changedByAgent: 'Cambiado por un agente',
    changedTitle: 'Un agente guardó la versión actual y no la aprobaste. Abre el historial de versiones para restaurar la versión que aprobaste por última vez.',
    you: 'Tú (editor)',
    agent: (name: string) => `Agente ${name}`,
    anAgent: 'Un agente',
    current: 'Actual',
    deployed: 'Desplegada',
    notApproved: 'No aprobada por ti',
    lastApproved: 'Última aprobada por ti',
    fromDraft: 'De un borrador',
    noRollback: 'No hay ninguna versión aprobada por ti a la que restaurar (no se conserva ninguna).',
    nothingToRollBack: 'La versión actual es la que aprobaste por última vez: no hay nada que restaurar.',
    rollbackConfirm: (name: string, last: number, current: number, running: boolean) =>
      `«${name}» vuelve a la versión ${last}, la que aprobaste por última vez: se guarda de nuevo como versión nueva, después de la versión ${current}. Las versiones posteriores siguen en el historial.` +
      (running ? ' El flujo está en marcha, así que se vuelve a desplegar con esa versión.' : ' El flujo no está en marcha; no se despliega nada.')
  },

  approveFailed: 'No se pudo aprobar el borrador. Inténtalo de nuevo.',
  rejectFailed: 'No se pudo rechazar el borrador. Inténtalo de nuevo.',

  list: {
    title: 'Borradores',
    intro: 'Flujos y cambios que proponen los agentes. Nada se guarda ni se ejecuta hasta que lo apruebes; revisa antes las alertas y los cambios.',
    filterWaiting: 'Pendientes',
    filterDecided: 'Decididos',
    filterAll: 'Todos',
    show: 'Mostrar',
    statusLabel: 'Estado del borrador',
    refreshLabel: 'Actualizar borradores',
    refresh: 'Actualizar',
    outdated: 'Los borradores cambiaron desde que se cargó esta lista. Al actualizar aparecen, empezando de nuevo por los más recientes.',
    loading: 'Cargando borradores…',
    loadFailed: 'No se pudieron cargar los borradores.',
    loadMoreFailed: 'No se pudieron cargar más borradores.',
    empty: 'No hay borradores que mostrar.',
    emptyAuthorOff: 'No hay borradores pendientes. La autoría de agentes está desactivada (mcp.author_enabled), así que los agentes no pueden proponer flujos.',
    emptyWaiting: 'No hay borradores pendientes. Los agentes con el alcance author pueden proponer flujos aquí.',
    articleLabel: (name: string) => `Borrador ${name}`,
    reviewLabel: (isNew: boolean, name: string) => `Revisar el borrador ${isNew ? 'del flujo nuevo ' : 'de '}${name}`,
    alerts: (n: number) => count(n, 'alerta', 'alertas'),
    newFlow: 'Flujo nuevo',
    riskPrefix: 'Riesgo',
    token: 'Token',
    written: 'Escrito',
    version: 'Versión',
    decided: 'Decidido',
    note: 'Nota escrita por el agente (se muestra como texto)',
    review: 'Revisar…',
    view: 'Ver',
    loadMore: 'Cargar más'
  },

  before: 'Antes',
  after: 'Después',
  valuesInDiff: 'Los valores están en los cambios del nodo, más abajo.',
  unchangedValue: 'Valor (el ajuste en sí no cambió)',
  valueNotShown: 'El valor de este ajuste no se muestra aquí.',
  change: { added: 'Añadido', removed: 'Eliminado', changed: 'Modificado' },
  nodeAria: (change: string, type: string, id: string) => `Nodo ${change.toLowerCase()} ${type} ${id}`,
  moved: 'Movido en el lienzo.',
  fieldsCaption: (id: string) => `Campos modificados del nodo ${id}`,
  field: 'Campo',
  destination: 'Destino',
  credential: 'Credencial',
  notSet: '(sin definir)',
  removed: '(eliminado)',
  sensitiveTitle: 'Valor sensible: nunca se muestra',
  connections: 'Conexiones',
  connectionsAria: (label: string) => `${label}: conexiones`,

  review: {
    allDrafts: 'Todos los borradores',
    openInEditor: 'Abrir el flujo en el editor',
    loading: 'Cargando el borrador…',
    notFound: 'Este borrador no existe. Los borradores decididos se conservan 30 días.',
    loadFailed: 'No se pudo cargar el borrador.',
    retry: 'Reintentar',
    headingNew: 'Borrador de un flujo nuevo: ',
    headingFor: 'Borrador de ',
    writtenBy: 'Escrito por el token de agente',
    written: 'Escrito',
    version: 'Versión',
    flow: 'Flujo',
    running: 'En marcha',
    notRunning: 'Detenido',
    decided: 'Decidido',
    reason: 'Motivo: ',
    openFlow: 'Abrir el flujo',
    goToDecision: 'Ir a la decisión',
    noteHeading: 'Nota escrita por el agente',
    noteShown: (hidden: number) =>
      `Se muestra como texto, tal como la envió el agente${hidden ? `; ${hidden} carácter(es) oculto(s) aparecen resaltados como escapes \\u` : ''}.`,
    risk: 'Riesgo',
    riskBefore: 'Antes',
    riskAfter: 'Después',
    alerts: (n: number) => `Alertas (${n})`,
    issues: (counts: string) => `Problemas: ${counts}`,
    noIssues: 'El borrador pasa las mismas comprobaciones que el despliegue.',
    changesHelp: 'Las credenciales nunca se muestran; los ajustes marcados como Destino indican adónde envía datos o se conecta el flujo.',
    nameAndDescription: 'Nombre y descripción',
    field: 'Campo',
    before: 'Antes',
    after: 'Después',
    empty: '(vacío)',
    changesLabel: 'Cambios',
    approvedHeading: (version: number) => `Respecto a la versión que aprobaste por última vez (versión ${version})`,
    approvedHelp: 'Un agente guardó la versión actual y no la aprobaste. Estos son los cambios desde la versión que aprobaste: incluyen lo que un agente cambió antes de este borrador.',
    approvedPrefix: 'Aprobada',
    withDraftPrefix: 'Con este borrador',
    noAlertsApproved: 'Sin alertas respecto a la versión que aprobaste.',
    sinceApproved: 'Desde la versión que aprobaste',
    proposedFlow: 'Flujo propuesto (JSON tal como lo escribió el agente; credenciales ocultas)',
    decision: 'Decisión',
    deployNow: 'Desplegar ahora',
    deployRunning: 'El flujo está en marcha, así que aprobarlo siempre lo vuelve a desplegar con la versión nueva.',
    deployHelp: 'Inicia el flujo con la versión nueva justo después de guardarla. Si no, se guarda y sigue detenido.',
    approveDeploy: 'Aprobar y desplegar…',
    approveSave: 'Aprobar y guardar…',
    reasonLabel: 'Motivo del rechazo (opcional, lo ve el agente)',
    reasonPlaceholder: 'Qué debería cambiar el agente',
    characters: (n: number, max: number) => `${n} / ${max} caracteres`,
    reject: 'Rechazar…',
    noteHiddenWarning: (n: number) => ` La nota del agente contiene ${n} carácter(es) oculto(s), resaltados como escapes \\u.`,
    approveTitle: '¿Aprobar este borrador?',
    confirmDeploy: 'Aprobar y desplegar',
    confirmSave: 'Aprobar y guardar',
    savingDeploying: 'Guardando y desplegando…',
    saving: 'Guardando…',
    approvedAction: 'Aprobado',
    notApproved: (message: string) => `No aprobado: ${message}`,
    rejectTitle: '¿Rechazar este borrador?',
    rejectText: (token: string, withReason: boolean) =>
      `No se guarda nada. Se avisa al agente «${token}» de que el borrador se rechazó${withReason ? ', con tu motivo' : ''}.`,
    rejectConfirm: 'Rechazar borrador',
    rejecting: 'Rechazando…',
    rejected: 'Borrador rechazado. El agente lo verá la próxima vez que consulte el borrador.',
    notRejected: (message: string) => `No rechazado: ${message}`,
    tryAgain: 'inténtalo de nuevo'
  }
}

export default es
