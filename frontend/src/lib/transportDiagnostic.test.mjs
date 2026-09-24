import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import ts from 'typescript'

const source = readFileSync(new URL('./transportDiagnostic.ts', import.meta.url), 'utf8')
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } }).outputText
const moduleURL = `data:text/javascript;base64,${Buffer.from(compiled).toString('base64')}`
const { formatDiagnosticBody, sortedDiagnosticHeaders } = await import(moduleURL)

test('diagnostic body pretty-prints JSON objects', () => {
  const result = formatDiagnosticBody('{"model":"gpt-5.6","input":[1]}')
  assert.equal(result.format, 'json')
  assert.match(result.text, /\n  "model": "gpt-5.6"/)
})

test('diagnostic body formats newline-delimited websocket frames', () => {
  const result = formatDiagnosticBody('{"type":"response.created"}\n{"type":"response.completed"}')
  assert.equal(result.format, 'ndjson')
  assert.match(result.text, /response\.created[\s\S]+response\.completed/)
})

test('diagnostic body formats JSON data inside SSE frames', () => {
  const result = formatDiagnosticBody('event: message\ndata: {"type":"response.output_text.delta","delta":"hi"}\n\ndata: [DONE]')
  assert.equal(result.format, 'sse')
  assert.match(result.text, /data:\n\{\n  "type"/)
  assert.match(result.text, /data: \[DONE\]/)
})

test('diagnostic headers sort case-insensitively', () => {
  assert.deepEqual(sortedDiagnosticHeaders({ Zebra: ['2'], accept: ['1'] }).map(([name]) => name), ['accept', 'Zebra'])
})
