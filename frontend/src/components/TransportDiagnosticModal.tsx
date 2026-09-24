import { useEffect, useMemo, useState } from 'react'
import { Check, Copy, FileJson, FileText, Network, ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { CodexTransportDiagnostic, CodexTransportDiagnosticResponse } from '../types'
import { formatDiagnosticBody, sortedDiagnosticHeaders } from '../lib/transportDiagnostic'
import Modal from './Modal'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

type DiagnosticModalProps = {
  show: boolean
  loading: boolean
  data: CodexTransportDiagnosticResponse | null
  onClose: () => void
}

function CopyButton({ value }: { value: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1200)
    } catch {
      // Clipboard access can be blocked outside a secure browser context.
    }
  }
  return (
    <Button type="button" size="icon" variant="ghost" className="size-7" disabled={!value} title={t('common.copy')} onClick={() => void copy()}>
      {copied ? <Check className="size-3.5 text-emerald-500" /> : <Copy className="size-3.5" />}
    </Button>
  )
}

function HeadersPanel({ title, headers }: { title: string; headers?: Record<string, string[]> }) {
  const { t } = useTranslation()
  const entries = useMemo(() => sortedDiagnosticHeaders(headers), [headers])
  const copyValue = entries.map(([name, values]) => `${name}: ${values.join(', ')}`).join('\n')
  return (
    <section className="min-w-0 overflow-hidden rounded-xl border bg-card">
      <div className="flex h-10 items-center gap-2 border-b bg-muted/35 px-3">
        <Network className="size-3.5 text-muted-foreground" />
        <h4 className="text-xs font-semibold">{title}</h4>
        <span className="ml-auto text-[10px] tabular-nums text-muted-foreground">{entries.length}</span>
        <CopyButton value={copyValue} />
      </div>
      {entries.length ? (
        <div className="max-h-56 overflow-auto">
          {entries.map(([name, values]) => (
            <div key={name} className="grid grid-cols-[minmax(110px,0.38fr)_minmax(0,1fr)] gap-3 border-b px-3 py-2 text-[11px] last:border-b-0">
              <div className="break-all font-mono font-semibold text-muted-foreground">{name}</div>
              <div className="break-all font-mono text-foreground/85">{values.join(', ')}</div>
            </div>
          ))}
        </div>
      ) : <div className="px-3 py-5 text-center text-xs text-muted-foreground">{t('diagnostics.emptyHeaders')}</div>}
    </section>
  )
}

function BodyPanel({ title, body, truncated }: { title: string; body?: string; truncated?: boolean }) {
  const { t } = useTranslation()
  const formatted = useMemo(() => formatDiagnosticBody(body), [body])
  const Icon = formatted.format === 'json' || formatted.format === 'ndjson' || formatted.format === 'sse' ? FileJson : FileText
  return (
    <section className="min-w-0 overflow-hidden rounded-xl border bg-card">
      <div className="flex h-10 items-center gap-2 border-b bg-muted/35 px-3">
        <Icon className="size-3.5 text-muted-foreground" />
        <h4 className="text-xs font-semibold">{title}</h4>
        {formatted.format !== 'empty' ? <Badge variant="outline" className="ml-auto h-5 border-transparent bg-muted px-1.5 text-[9px] uppercase text-muted-foreground">{formatted.format}</Badge> : <span className="ml-auto" />}
        {truncated ? <Badge variant="outline" className="h-5 border-amber-500/20 bg-amber-500/10 px-1.5 text-[9px] text-amber-700 dark:text-amber-300">{t('diagnostics.truncated')}</Badge> : null}
        <CopyButton value={formatted.text} />
      </div>
      {formatted.text ? (
        <pre className="max-h-[32rem] overflow-auto whitespace-pre-wrap break-words p-3 font-mono text-[11px] leading-relaxed text-foreground/90">{formatted.text}</pre>
      ) : <div className="px-3 py-8 text-center text-xs text-muted-foreground">{t('diagnostics.emptyBody')}</div>}
    </section>
  )
}

function CaptureView({ capture }: { capture: CodexTransportDiagnostic }) {
  const { t, i18n } = useTranslation()
  const timestamp = capture.captured_at ? new Date(capture.captured_at).toLocaleString(i18n.language) : '-'
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2 rounded-xl border bg-muted/20 p-3 text-xs">
        <Badge variant="outline" className="font-mono uppercase">{capture.transport}</Badge>
        <Badge variant="outline" className="font-mono">{capture.stage || '-'}</Badge>
        {capture.status ? <Badge variant="outline" className={cn('font-mono', capture.status >= 400 && 'border-red-500/25 bg-red-500/10 text-red-600 dark:text-red-300')}>HTTP {capture.status}</Badge> : null}
        <span className="text-muted-foreground">{timestamp}</span>
        {capture.account_id ? <span className="font-mono text-[10px] text-muted-foreground">account #{capture.account_id}</span> : null}
        {capture.proxy ? <span className="max-w-full break-all font-mono text-[10px] text-muted-foreground">proxy {capture.proxy}</span> : null}
        {capture.method || capture.url ? <span className="min-w-0 break-all font-mono text-[11px]">{[capture.method, capture.url].filter(Boolean).join(' ')}</span> : null}
        {capture.upstream_request_id ? <span className="max-w-full break-all font-mono text-[10px] text-muted-foreground">upstream {capture.upstream_request_id}</span> : null}
        <span className="ml-auto inline-flex items-center gap-1 text-[10px] text-emerald-600 dark:text-emerald-400"><ShieldCheck className="size-3" />{t('diagnostics.redacted')}</span>
      </div>
      {capture.error ? <div className="rounded-xl border border-red-500/20 bg-red-500/8 px-3 py-2 text-xs text-red-700 dark:text-red-300">{capture.error}</div> : null}
      <div className="grid min-w-0 gap-4 xl:grid-cols-2">
        <div className="min-w-0 space-y-4">
          <HeadersPanel title={t('diagnostics.requestHeaders')} headers={capture.request_headers} />
          <BodyPanel title={t('diagnostics.requestBody')} body={capture.request_body} truncated={capture.request_truncated} />
        </div>
        <div className="min-w-0 space-y-4">
          <HeadersPanel title={t('diagnostics.responseHeaders')} headers={capture.response_headers} />
          <BodyPanel title={t('diagnostics.responseBody')} body={capture.response_body} truncated={capture.response_truncated} />
        </div>
      </div>
    </div>
  )
}

export default function TransportDiagnosticModal({ show, loading, data, onClose }: DiagnosticModalProps) {
  const { t } = useTranslation()
  const [selected, setSelected] = useState(0)
  useEffect(() => setSelected(0), [data?.request_id])
  const captures = data?.captures ?? []
  const active = captures[Math.min(selected, Math.max(0, captures.length - 1))]
  return (
    <Modal
      show={show}
      title={t('diagnostics.title')}
      onClose={onClose}
      contentClassName="sm:max-w-[min(96vw,1180px)]"
      bodyClassName="bg-muted/10"
    >
      <div className="mb-4 break-all font-mono text-[11px] text-muted-foreground">{data?.request_id ?? ''}</div>
      {loading ? <div className="py-16 text-center text-sm text-muted-foreground">{t('common.loading')}</div> : null}
      {!loading && captures.length === 0 ? (
        <div className="rounded-xl border border-dashed py-16 text-center text-sm text-muted-foreground">
          {data?.enabled ? t('diagnostics.noCapture') : t('diagnostics.captureDisabled')}
        </div>
      ) : null}
      {!loading && captures.length > 1 ? (
        <div className="mb-4 flex gap-2 overflow-x-auto pb-1">
          {captures.map((capture, index) => (
            <Button key={`${capture.captured_at}-${index}`} type="button" size="sm" variant={selected === index ? 'default' : 'outline'} onClick={() => setSelected(index)}>
              {t('diagnostics.attempt', { index: index + 1 })} · {capture.transport}
            </Button>
          ))}
        </div>
      ) : null}
      {!loading && active ? <CaptureView capture={active} /> : null}
    </Modal>
  )
}
