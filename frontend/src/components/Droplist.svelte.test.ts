// SPDX-License-Identifier: AGPL-3.0-only
//
// Settings' "drop list" group at the component (#1225, #461): the
// drift line per router (and the honest no-routers state), the pull
// key's present/absent rows and its one-time mint reveal, the add
// form's success/400/warning paths, the entries list and its two-step
// remove, the setup card's four blocks, and #1225's flag-drawer draft
// handoff.
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'

vi.mock('../lib/api', () => ({
  createDroplistEntry: vi.fn(),
  deleteDroplistEntry: vi.fn(),
  mintDroplistKey: vi.fn(),
  revokeDroplistKey: vi.fn(),
}))

import { createDroplistEntry, deleteDroplistEntry, mintDroplistKey, revokeDroplistKey } from '../lib/api'
import { droplistNavState } from '../lib/droplistNav.svelte'
import { wizardState } from '../lib/wizard.svelte'
import Droplist from './Droplist.svelte'
import type { DroplistResponse } from '../lib/types'

function resp(over: Partial<DroplistResponse> = {}): DroplistResponse {
  return {
    listName: 'mikroview-drops',
    entries: [],
    key: { present: false },
    ownRangesKnown: true,
    setup: {
      scheduler: '/system scheduler add name=mikroview-drop-pull ... <DROP-LIST-KEY> ...',
      rule: '/ip firewall filter add chain=forward src-address-list=mikroview-drops action=drop',
      disableRule: '/ip firewall filter disable [find where address-list=mikroview-drops]',
      emptyList: '/ip firewall address-list remove [find where list=mikroview-drops]',
    },
    ...over,
  }
}

const entry = {
  cidr: '203.0.113.0/24',
  addedBy: 'tom',
  addedAt: '2026-09-14T00:00:00Z',
  reason: 'ssh brute force',
}

beforeEach(() => {
  vi.resetAllMocks()
  droplistNavState.pendingDraft = null
})

describe('entries', () => {
  it('renders one row per entry, and the empty state when there are none', () => {
    render(Droplist, { props: { resp: resp({ entries: [entry] }), onrefresh: vi.fn() } })
    expect(screen.getByText('203.0.113.0/24')).toBeTruthy()
    expect(screen.getByText('ssh brute force')).toBeTruthy()
    expect(screen.getByText(/by tom/)).toBeTruthy()
  })

  it('says nothing is dropped, and points at block… on a flag, when the list is empty', () => {
    render(Droplist, { props: { resp: resp(), onrefresh: vi.fn() } })
    expect(screen.getByText(/nothing dropped — add an address here, or use Block… on a flag/)).toBeTruthy()
  })

  it('offers a "from flag" link only where the entry carries a flagID', () => {
    render(Droplist, {
      props: {
        resp: resp({ entries: [entry, { ...entry, cidr: '198.51.100.9', flagID: 'f1' }] }),
        onrefresh: vi.fn(),
      },
    })
    expect(screen.getAllByRole('button', { name: 'from flag' })).toHaveLength(1)
  })

  it('removes only behind a two-step confirm', async () => {
    vi.mocked(deleteDroplistEntry).mockResolvedValue(null)
    const onrefresh = vi.fn(async () => {})
    render(Droplist, { props: { resp: resp({ entries: [entry] }), onrefresh } })

    await fireEvent.click(screen.getByRole('button', { name: 'remove' }))
    expect(deleteDroplistEntry).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: /confirm/ })).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: /confirm/ }))
    expect(deleteDroplistEntry).toHaveBeenCalledWith('203.0.113.0/24')
    expect(onrefresh).toHaveBeenCalled()
  })
})

describe('the drift line', () => {
  it('reads holds N of M and when each router confirmed', () => {
    render(Droplist, {
      props: {
        resp: resp({ routers: [{ device: 'rb5009', held: 40, total: 42, confirmedAt: '2026-09-14T00:00:00Z' }] }),
        onrefresh: vi.fn(),
      },
    })
    expect(screen.getByText('rb5009')).toBeTruthy()
    expect(screen.getByText(/holds 40 of 42/)).toBeTruthy()
  })

  it('says nothing heard yet, without reading it as agreement, when no router has reported', () => {
    render(Droplist, { props: { resp: resp(), onrefresh: vi.fn() } })
    expect(screen.getByText(/nothing heard yet — no router has reported its address lists/)).toBeTruthy()
  })
})

describe('the pull key', () => {
  it('absent: offers to mint one', () => {
    render(Droplist, { props: { resp: resp({ key: { present: false } }), onrefresh: vi.fn() } })
    expect(screen.getByText(/no key — the router cannot fetch the list until one is minted/)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'mint key' })).toBeTruthy()
  })

  it('present: states who minted it and when it last fetched, or never', () => {
    render(Droplist, {
      props: {
        resp: resp({
          key: { present: true, createdAt: '2026-09-13T00:00:00Z', createdBy: 'tom', lastUsedAt: undefined },
        }),
        onrefresh: vi.fn(),
      },
    })
    expect(screen.getByText(/minted .* by tom/)).toBeTruthy()
    expect(screen.getByText(/never fetched/)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'replace key' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'revoke' })).toBeTruthy()
  })

  it('revoke needs a two-step confirm', async () => {
    vi.mocked(revokeDroplistKey).mockResolvedValue(null)
    const onrefresh = vi.fn(async () => {})
    render(Droplist, {
      props: { resp: resp({ key: { present: true, createdAt: '2026-09-13T00:00:00Z', createdBy: 'tom' } }), onrefresh },
    })

    await fireEvent.click(screen.getByRole('button', { name: 'revoke' }))
    expect(revokeDroplistKey).not.toHaveBeenCalled()
    await fireEvent.click(screen.getByRole('button', { name: /confirm/ }))
    expect(revokeDroplistKey).toHaveBeenCalled()
    expect(onrefresh).toHaveBeenCalled()
  })

  it('minting reveals the key and the filled-in scheduler once', async () => {
    vi.mocked(mintDroplistKey).mockResolvedValue({
      key: 'the-fresh-key',
      createdAt: '2026-09-14T00:00:00Z',
      scheduler: '/system scheduler add name=mikroview-drop-pull ... the-fresh-key ...',
    })
    const onrefresh = vi.fn(async () => {})
    render(Droplist, { props: { resp: resp(), onrefresh } })

    await fireEvent.click(screen.getByRole('button', { name: 'mint key' }))
    expect(screen.getByText('the-fresh-key')).toBeTruthy()
    expect(screen.getByText(/\/system scheduler add name=mikroview-drop-pull \.\.\. the-fresh-key/)).toBeTruthy()
    expect(screen.getByText(/shown once/)).toBeTruthy()
    expect(onrefresh).toHaveBeenCalled()

    await fireEvent.click(screen.getByRole('button', { name: 'done' }))
    expect(screen.queryByText('the-fresh-key')).toBeNull()
  })

  // #1260: the mint call used to build the router's pull command from
  // this browser tab's own window.location.host rather than
  // wizardState.address (#1213 -- the operator's own saved answer to
  // "what address can your router reach mikroview on?"), so a router
  // ended up pointed at whatever host the admin happened to be browsing
  // from rather than the address they actually saved.
  it('mints the key against the operator saved address, not this tab\'s own host', async () => {
    wizardState.address = 'operator-saved.example:8443'
    vi.mocked(mintDroplistKey).mockResolvedValue({
      key: 'the-fresh-key',
      createdAt: '2026-09-14T00:00:00Z',
      scheduler: '/system scheduler add name=mikroview-drop-pull ... the-fresh-key ...',
    })
    render(Droplist, { props: { resp: resp(), onrefresh: vi.fn(async () => {}) } })

    await fireEvent.click(screen.getByRole('button', { name: 'mint key' }))
    expect(mintDroplistKey).toHaveBeenCalledWith('operator-saved.example:8443')
  })
})

describe('the setup card', () => {
  it('is closed by default, and the toggle reveals all four blocks', async () => {
    render(Droplist, { props: { resp: resp(), onrefresh: vi.fn() } })
    expect(screen.queryByText(/printed, never applied/)).toBeNull()

    await fireEvent.click(screen.getByRole('button', { name: 'setup ▸' }))
    expect(screen.getByText(/printed, never applied — paste these on the router yourself/)).toBeTruthy()
    expect(screen.getByText('scheduler')).toBeTruthy()
    expect(screen.getByText('drop rule')).toBeTruthy()
    expect(screen.getByText('emergency: disable the rule')).toBeTruthy()
    expect(screen.getByText('emergency: empty the list')).toBeTruthy()
  })
})

describe('the own-ranges hint', () => {
  it('warns that new entries go unchecked while no router has reported its ranges', () => {
    render(Droplist, { props: { resp: resp({ ownRangesKnown: false }), onrefresh: vi.fn() } })
    expect(
      screen.getByText(/no router has reported its addresses yet, so new entries are not checked/),
    ).toBeTruthy()
  })

  it('says nothing when a router has', () => {
    render(Droplist, { props: { resp: resp({ ownRangesKnown: true }), onrefresh: vi.fn() } })
    expect(screen.queryByText(/not checked against the router's own ranges/)).toBeNull()
  })
})

describe('the add form', () => {
  it('adds, then clears the form and refreshes', async () => {
    vi.mocked(createDroplistEntry).mockResolvedValue({ ...entry, cidr: '198.51.100.0/24' })
    const onrefresh = vi.fn(async () => {})
    render(Droplist, { props: { resp: resp(), onrefresh } })

    await fireEvent.input(screen.getByLabelText('address'), { target: { value: '198.51.100.0/24' } })
    await fireEvent.input(screen.getByLabelText('reason'), { target: { value: 'why' } })
    await fireEvent.click(screen.getByRole('button', { name: 'add' }))

    expect(createDroplistEntry).toHaveBeenCalledWith({ cidr: '198.51.100.0/24', reason: 'why', flagID: undefined })
    expect(onrefresh).toHaveBeenCalled()
    expect((screen.getByLabelText('address') as HTMLInputElement).value).toBe('')
    expect((screen.getByLabelText('reason') as HTMLInputElement).value).toBe('')
  })

  it('shows a 400 message inline, without clearing the form', async () => {
    vi.mocked(createDroplistEntry).mockResolvedValue('the address is not valid')
    render(Droplist, { props: { resp: resp(), onrefresh: vi.fn() } })

    await fireEvent.input(screen.getByLabelText('address'), { target: { value: 'not-an-address' } })
    await fireEvent.input(screen.getByLabelText('reason'), { target: { value: 'why' } })
    await fireEvent.click(screen.getByRole('button', { name: 'add' }))

    expect(screen.getByText('the address is not valid')).toBeTruthy()
    expect((screen.getByLabelText('address') as HTMLInputElement).value).toBe('not-an-address')
  })

  it('shows a warning alongside a successful add, not as an error', async () => {
    vi.mocked(createDroplistEntry).mockResolvedValue({
      ...entry,
      cidr: '198.51.100.0/24',
      warning: "this falls inside the router's own ranges",
    })
    render(Droplist, { props: { resp: resp(), onrefresh: vi.fn(async () => {}) } })

    await fireEvent.input(screen.getByLabelText('address'), { target: { value: '198.51.100.0/24' } })
    await fireEvent.input(screen.getByLabelText('reason'), { target: { value: 'why' } })
    await fireEvent.click(screen.getByRole('button', { name: 'add' }))

    const warning = screen.getByText("this falls inside the router's own ranges")
    expect(warning.className).not.toMatch(/err/)
  })
})

describe('a pending draft from a flag\'s block… (#1225)', () => {
  it('prefills the form and focuses the address input, without submitting', () => {
    droplistNavState.pendingDraft = { cidr: '203.0.113.5', reason: 'distributed brute-force from 203.0.113.5', flagID: 'f1' }
    render(Droplist, { props: { resp: resp(), onrefresh: vi.fn() } })
    flushSync()

    expect((screen.getByLabelText('address') as HTMLInputElement).value).toBe('203.0.113.5')
    expect((screen.getByLabelText('reason') as HTMLInputElement).value).toBe('distributed brute-force from 203.0.113.5')
    expect(createDroplistEntry).not.toHaveBeenCalled()
    expect(droplistNavState.pendingDraft).toBeNull()
  })
})
