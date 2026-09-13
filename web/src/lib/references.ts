// Only advertise syntax implemented by the corresponding node field.
export type ReferenceMode = 'expression' | 'template' | 'secret' | 'name' | undefined
const credentials: Record<string, string[]> = {
 'action-telegram': ['botToken'], 'action-notify': ['url'], 'action-database': ['dsn'],
 'action-email': ['username','password'], 'action-mqtt': ['username','password'], 'trigger-mqtt': ['username','password']
}
const templates: Record<string, string[]> = {
 'process-template': ['template'], 'action-http': ['url','body'], 'action-email': ['subject','body'],
 'action-telegram': ['message'], 'action-notify': ['message'], 'action-mqtt': ['topic','payload'], 'action-file': ['content']
}
const expressions: Record<string, string[]> = {
 'process-transform': ['expression'], 'process-switch': ['condition'], 'process-filter': ['condition'], 'set-var': ['value']
}
export function referenceMode(nodeType: string, field: string): ReferenceMode {
 if (credentials[nodeType]?.includes(field)) return 'secret'
 if (templates[nodeType]?.includes(field)) return 'template'
 if (expressions[nodeType]?.includes(field)) return 'expression'
 if (['get-var','set-var'].includes(nodeType) && field === 'key') return 'name'
}
export interface Reference { label: string; value: string; group: string }
export function referenceOptions(mode: ReferenceMode, names: string[], secrets: string[]): Reference[] {
 if (!mode) return []
 if (mode === 'secret') return secrets.map(name => ({ label:name, value:`{{secret.${name}}}`, group:'Secret name' }))
 if (mode === 'name') return names.map(name => ({ label:name, value:name, group:'Global variable' }))
 const globals = names.filter(name => mode !== 'template' || /^[A-Za-z0-9_.-]+$/.test(name)).map(name => ({ label:name, value:mode === 'expression' ? `vars[${JSON.stringify(name)}]` : `{{vars.${name}}}`, group:'Global variable' }))
 return [...globals, ...['payload','topic','meta','id','timestamp'].filter(name => mode !== 'template' || name !== 'meta').map(name => ({label:name,value:mode === 'template' ? `{{${name}}}` : name,group:'Message'}))]
}
