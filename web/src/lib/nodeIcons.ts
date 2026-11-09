// Node icons: NodeTypeInfo.Icon names from the Go nodes (kebab-case lucide
// names) map to the lucide component names stored on node types and canvas
// nodes, which map to the components the palette and the canvas render.
import type { Component } from 'vue'
import {
  Activity, Bell, Braces, Bug, Clock, Code, Cpu, Database, FileText, Filter, Gauge, GitBranch, Globe, HelpCircle,
  Hourglass, Layers, Mail, Merge, Play, Radio, Reply, Rss, Send, Terminal, Thermometer, Timer, ToggleLeft,
  ToggleRight, Type, Variable, Wand2, Webhook, Wrench, Zap
} from 'lucide-vue-next'

// Backend icon names whose lucide name differs from their PascalCase form.
const ALIASES: Record<string, string> = {
  schedule: 'Clock',
  text: 'FileText'
}

const COMPONENTS: Record<string, Component> = {
  Activity, Bell, Braces, Bug, Clock, Code, Cpu, Database, FileText, Filter, Gauge, GitBranch, Globe, HelpCircle,
  Hourglass, Layers, Mail, Merge, Play, Radio, Reply, Rss, Send, Terminal, Thermometer, Timer, ToggleLeft,
  ToggleRight, Type, Variable, Wand2, Webhook, Wrench, Zap
}

function pascal(name: string): string {
  return name.split('-').map(part => part.charAt(0).toUpperCase() + part.slice(1)).join('')
}

/** The lucide component name for a backend icon name ("wand-2" → "Wand2"); HelpCircle when unknown. */
export function lucideIconName(backend: string): string {
  const name = Object.prototype.hasOwnProperty.call(ALIASES, backend) ? ALIASES[backend] : pascal(backend)
  return Object.prototype.hasOwnProperty.call(COMPONENTS, name) ? name : 'HelpCircle'
}

/** The component for a lucide icon name; HelpCircle when unknown. */
export function nodeIcon(name: string): Component {
  return Object.prototype.hasOwnProperty.call(COMPONENTS, name) ? COMPONENTS[name] : HelpCircle
}
