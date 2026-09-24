export type DiagnosticBodyFormat = 'empty' | 'json' | 'ndjson' | 'sse' | 'text'

export interface FormattedDiagnosticBody {
  format: DiagnosticBodyFormat
  text: string
}

function prettyJSON(value: string): string | null {
  try {
    return JSON.stringify(JSON.parse(value), null, 2)
  } catch {
    return null
  }
}

function formatSSE(value: string): string | null {
  const frames = value.split(/\r?\n\r?\n/).filter((frame) => frame.trim())
  if (frames.length === 0 || !frames.some((frame) => /^data:/m.test(frame))) return null
  return frames.map((frame) => frame.split(/\r?\n/).map((line) => {
    if (!line.startsWith('data:')) return line
    const data = line.slice(5).trimStart()
    if (!data || data === '[DONE]') return line
    const formatted = prettyJSON(data)
    return formatted ? `data:\n${formatted}` : line
  }).join('\n')).join('\n\n')
}

export function formatDiagnosticBody(value?: string): FormattedDiagnosticBody {
  const raw = value?.trim() ?? ''
  if (!raw) return { format: 'empty', text: '' }

  const json = prettyJSON(raw)
  if (json) return { format: 'json', text: json }

  const sse = formatSSE(raw)
  if (sse) return { format: 'sse', text: sse }

  const lines = raw.split(/\r?\n/).filter((line) => line.trim())
  const formattedLines = lines.map((line) => prettyJSON(line))
  if (lines.length > 1 && formattedLines.every((line) => line !== null)) {
    return { format: 'ndjson', text: formattedLines.join('\n\n') }
  }

  return { format: 'text', text: raw }
}

export function sortedDiagnosticHeaders(headers?: Record<string, string[]>): Array<[string, string[]]> {
  return Object.entries(headers ?? {}).sort(([left], [right]) => left.localeCompare(right, undefined, { sensitivity: 'base' }))
}
