import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { NodeTypeConfig, NodeCategory, ConfigField } from '@/types'
import { useApi } from '@/composables/useApi'

// Map backend categories to frontend categories
const categoryMap: Record<string, NodeCategory> = {
  input: 'trigger',
  processing: 'process',
  output: 'action'
}

// Map backend icon names to Lucide icon names
const iconMap: Record<string, string> = {
  timer: 'Timer',
  schedule: 'Clock',
  terminal: 'Terminal',
  text: 'FileText',
  hourglass: 'Hourglass',
  play: 'Play',
  bug: 'Bug',
  globe: 'Globe',
  'wand-2': 'Wand2',
  webhook: 'Webhook',
  filter: 'Filter',
  'git-branch': 'GitBranch',
  type: 'Type',
  'file-text': 'FileText',
  bell: 'Bell',
  send: 'Send',
  mail: 'Mail',
  variable: 'Variable',
  activity: 'Activity'
}

export const useNodeTypesStore = defineStore('nodeTypes', () => {
  const api = useApi()

  const nodeTypes = ref<NodeTypeConfig[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)
  const initialized = ref(false)

  // Dedupe concurrent calls by returning the in-flight promise, so callers that
  // `await fetchNodeTypes()` reliably wait for completion even when a fetch was
  // already started fire-and-forget at store creation.
  let inFlight: Promise<void> | null = null

  function fetchNodeTypes(): Promise<void> {
    if (inFlight) return inFlight

    inFlight = (async () => {
      loading.value = true
      error.value = null
      try {
        const data = await api.getNodeTypes()

        // Transform API response to frontend format
        nodeTypes.value = data.nodeTypes.map((node: any) => ({
          id: node.type, // Use 'type' as the ID (e.g., 'trigger-interval')
          name: node.name,
          description: node.description,
          documentation: node.documentation || '',
          category: categoryMap[node.category] || node.category,
          icon: iconMap[node.icon] || 'HelpCircle',
          inputs: node.inputs || [],
          outputs: node.outputs || [],
          configSchema: buildConfigSchema(node)
        }))

        initialized.value = true
      } catch (e) {
        error.value = e instanceof Error ? e.message : 'Failed to fetch node types'
        console.error('Failed to fetch node types:', e)
      } finally {
        loading.value = false
        inFlight = null
      }
    })()

    return inFlight
  }

  // Convert a backend JSON config schema ({ properties, required }) into the
  // frontend ConfigField[] the config panel renders.
  function configSchemaFromBackend(schema: any): ConfigField[] {
    const required: string[] = schema.required || []
    const props = schema.properties || {}
    return Object.keys(props).map((key): ConfigField => {
      const p = props[key]
      const hasEnum = Array.isArray(p.enum) && p.enum.length > 0
      let type: ConfigField['type'] = 'text'
      if (p.type === 'number') type = 'number'
      else if (p.type === 'boolean') type = 'boolean'
      else if (hasEnum) type = 'select'
      return {
        key,
        label: p.title || key,
        type,
        required: required.includes(key),
        default: p.default,
        placeholder: p.description || '',
        options: hasEnum ? p.enum.map((v: string) => ({ label: v, value: v })) : undefined
      }
    })
  }

  // Build a node's config form from the backend-provided JSON schema. Every node
  // now exposes its schema via `config`, so there is no per-type fallback.
  function buildConfigSchema(node: any): NodeTypeConfig['configSchema'] {
    if (node.config && node.config.properties) {
      return configSchemaFromBackend(node.config)
    }
    return []
  }

  const triggers = computed(() =>
    nodeTypes.value.filter(n => n.category === 'trigger')
  )

  const processes = computed(() =>
    nodeTypes.value.filter(n => n.category === 'process')
  )

  const actions = computed(() =>
    nodeTypes.value.filter(n => n.category === 'action')
  )

  const getByCategory = (category: NodeCategory) => {
    return nodeTypes.value.filter(n => n.category === category)
  }

  const getById = (id: string) => {
    return nodeTypes.value.find(n => n.id === id)
  }

  const getCategoryColor = (category: NodeCategory): string => {
    switch (category) {
      case 'trigger':
        return 'trigger'
      case 'process':
        return 'process'
      case 'action':
        return 'action'
      default:
        return 'secondary'
    }
  }

  // Initialize on store creation
  fetchNodeTypes()

  return {
    nodeTypes,
    loading,
    error,
    initialized,
    triggers,
    processes,
    actions,
    fetchNodeTypes,
    getByCategory,
    getById,
    getCategoryColor
  }
})
