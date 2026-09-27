// SPDX-License-Identifier: AGPL-3.0-only
//
// #1347: the config editor's API client -- the lapsed-unlock 401 told
// apart from any other refusal, the server's filename read off the
// download, and the requests sent as the contract names them.

import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  createConfigSnapshot,
  deleteConfigSnapshot,
  downloadConfig,
  fetchConfigSnapshots,
  filenameFromDisposition,
  openConfigEditor,
  revealConfigSecrets,
  validateConfig,
} from './api'

afterEach(() => {
  vi.unstubAllGlobals()
})

function stub(res: Response) {
  const fetchMock = vi.fn(async () => res)
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('config editor API (#1347)', () => {
  it('opens with the password in a JSON POST carrying the CSRF header', async () => {
    const fetchMock = stub(new Response(JSON.stringify({ text: 'a: 1', path: '/c.yaml' }), { status: 200 }))
    const res = await openConfigEditor('hunter2')
    expect(typeof res).toBe('object')
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    expect(url).toBe('/api/config/editor/open')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body as string)).toEqual({ password: 'hunter2' })
    expect((init.headers as Record<string, string>)['X-Requested-With']).toBe('mikroview')
  })

  it('returns the server\'s words for a refused password', async () => {
    stub(new Response('wrong password', { status: 403 }))
    expect(await openConfigEditor('x')).toBe('wrong password')
  })

  it('reads a download\'s file and its name from Content-Disposition', async () => {
    stub(
      new Response('listen: {}\n', {
        status: 200,
        headers: { 'Content-Disposition': 'attachment; filename="config.v0.7.0.yaml"' },
      }),
    )
    const res = await downloadConfig('listen: {}\n')
    if (typeof res === 'string' || 'reauth' in res) throw new Error('expected a file')
    expect(res.filename).toBe('config.v0.7.0.yaml')
    expect(await res.blob.text()).toBe('listen: {}\n')
  })

  it('tells a lapsed unlock (401 {reauth:true}) apart from an ended session (plain 401)', async () => {
    stub(new Response(JSON.stringify({ reauth: true }), { status: 401 }))
    expect(await downloadConfig('x')).toEqual({ reauth: true })
    stub(new Response(JSON.stringify({ reauth: true }), { status: 401 }))
    expect(await revealConfigSecrets()).toEqual({ reauth: true })
    stub(new Response('', { status: 401 }))
    expect(await downloadConfig('x')).toBe('your session has expired — sign in again')
  })

  it('reveals the secrets map', async () => {
    stub(new Response(JSON.stringify({ secrets: { 'oidc.clientSecret': 's' } }), { status: 200 }))
    expect(await revealConfigSecrets()).toEqual({ secrets: { 'oidc.clientSecret': 's' } })
  })

  it('throws on a failed check rather than reporting no problems', async () => {
    stub(new Response('', { status: 500 }))
    await expect(validateConfig('x')).rejects.toThrow('the server could not do that (500)')
  })

  it('lists snapshots from a bare array or a {snapshots} envelope', async () => {
    const one = { id: 'a', when: '2026-09-27T10:00:00Z', by: 'tom', schema: 7, why: 'manual' }
    stub(new Response(JSON.stringify([one]), { status: 200 }))
    expect(await fetchConfigSnapshots()).toEqual([one])
    stub(new Response(JSON.stringify({ snapshots: [one] }), { status: 200 }))
    expect(await fetchConfigSnapshots()).toEqual([one])
  })

  it('keeps and deletes snapshots at the contract\'s routes', async () => {
    let fetchMock = stub(new Response('{}', { status: 201 }))
    expect(await createConfigSnapshot('a: 1', 'why')).toBeNull()
    let [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    expect(url).toBe('/api/config/snapshots')
    expect(JSON.parse(init.body as string)).toEqual({ text: 'a: 1', note: 'why' })

    fetchMock = stub(new Response(null, { status: 204 }))
    expect(await deleteConfigSnapshot('a/b')).toBeNull()
    ;[url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    expect(url).toBe('/api/config/snapshots/a%2Fb')
    expect(init.method).toBe('DELETE')
  })

  it('parses the filename forms a server sends, and never returns a path', () => {
    expect(filenameFromDisposition('attachment; filename="config.v0.7.0.yaml"')).toBe('config.v0.7.0.yaml')
    expect(filenameFromDisposition('attachment; filename=config.yaml')).toBe('config.yaml')
    expect(filenameFromDisposition("attachment; filename*=UTF-8''config%20v1.yaml")).toBe('config v1.yaml')
    expect(filenameFromDisposition('attachment; filename="../../etc/passwd"')).toBe('.._.._etc_passwd')
    expect(filenameFromDisposition(null)).toBeNull()
    expect(filenameFromDisposition('attachment')).toBeNull()
  })
})
