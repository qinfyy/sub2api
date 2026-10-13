import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAdminComplianceStore } from '../adminCompliance'
import type { AdminComplianceStatus } from '@/api/admin/compliance'

const api = vi.hoisted(() => ({ getStatus: vi.fn(), accept: vi.fn() }))
vi.mock('@/api/admin/compliance', () => ({ default: api }))
vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))

const status = (required: boolean) => ({ required, version: 'test' }) as AdminComplianceStatus
function deferred() {
  let resolve!: (value: AdminComplianceStatus) => void
  let reject!: (error: Error) => void
  const promise = new Promise<AdminComplianceStatus>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

describe('admin compliance request lifecycle', () => {
  beforeEach(() => { setActivePinia(createPinia()); vi.useFakeTimers(); vi.resetAllMocks() })
  afterEach(() => vi.useRealTimers())

  it('shares concurrent requests', async () => {
    const d = deferred()
    api.getStatus.mockReturnValue(d.promise)
    const store = useAdminComplianceStore()
    const a = store.fetchStatus()
    const b = store.fetchStatus()
    expect(api.getStatus).toHaveBeenCalledTimes(1)
    d.resolve(status(true))
    await Promise.all([a, b])
    expect(store.required).toBe(true)
    expect(store.loading).toBe(false)
  })

  it('cools down failures without approving compliance', async () => {
    api.getStatus.mockRejectedValue(new Error('offline'))
    const store = useAdminComplianceStore()
    await expect(store.fetchStatus()).rejects.toThrow('offline')
    await expect(store.fetchStatus()).rejects.toThrow('offline')
    expect(api.getStatus).toHaveBeenCalledTimes(1)
    expect(store.initialized).toBe(false)
    expect(store.status).toBeNull()
    await vi.advanceTimersByTimeAsync(5000)
    api.getStatus.mockResolvedValue(status(true))
    await store.fetchStatus()
    expect(api.getStatus).toHaveBeenCalledTimes(2)
    expect(store.required).toBe(true)
  })

  it('ignores an old login response and its finally handler', async () => {
    const old = deferred(), current = deferred()
    api.getStatus.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const store = useAdminComplianceStore()
    const oldRequest = store.fetchStatus()
    store.reset()
    const newRequest = store.fetchStatus()
    old.resolve(status(false))
    await expect(oldRequest).rejects.toThrow('session changed')
    expect(store.status).toBeNull()
    expect(store.loading).toBe(true)
    current.resolve(status(true))
    await newRequest
    expect(store.required).toBe(true)
  })

  it('does not overwrite acceptance with an earlier status response', async () => {
    const d = deferred()
    api.getStatus.mockReturnValue(d.promise)
    api.accept.mockResolvedValue(status(false))
    const store = useAdminComplianceStore()
    const pending = store.fetchStatus()
    await store.accept('confirmed')
    d.resolve(status(true))
    await pending
    expect(store.required).toBe(false)
    expect(store.initialized).toBe(true)
  })

  it('does not restore acceptance after logout', async () => {
    const d = deferred()
    api.accept.mockReturnValue(d.promise)
    const store = useAdminComplianceStore()
    const pending = store.accept('confirmed')
    store.reset()
    d.resolve(status(false))
    await expect(pending).rejects.toThrow('session changed')
    expect(store.status).toBeNull()
    expect(store.initialized).toBe(false)
  })

  it('keeps a newer acknowledgement requirement', async () => {
    const d = deferred()
    api.getStatus.mockReturnValue(d.promise)
    const store = useAdminComplianceStore()
    const pending = store.fetchStatus()
    store.requireAcknowledgement()
    d.resolve(status(false))
    await pending
    expect(store.required).toBe(true)
  })
})
