import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import SetupWizardView from '../SetupWizardView.vue'
import { install, testDatabase, testRedis } from '@/api/setup'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/setup', () => ({ install: vi.fn(), testDatabase: vi.fn(), testRedis: vi.fn() }))
vi.mock('@/api/client', () => ({ buildGatewayUrl: (path: string) => path }))

describe('SetupWizardView installation', () => {
  let wrapper: VueWrapper

  beforeEach(() => {
    vi.useFakeTimers()
    vi.resetAllMocks()
    vi.mocked(testDatabase).mockResolvedValue({ message: 'ok' })
    vi.mocked(testRedis).mockResolvedValue({ message: 'ok' })
    vi.mocked(install).mockResolvedValue({ message: 'ok', restart_required: true })
    wrapper = mount(SetupWizardView, { attachTo: document.body })
  })

  afterEach(() => {
    wrapper.unmount()
    document.body.replaceChildren()
    vi.clearAllTimers()
    vi.useRealTimers()
  })

  const button = (label: string) => {
    const result = wrapper.findAll('button').find(item => item.text() === label)
    if (!result) throw new Error(`Missing button ${label}`)
    return result
  }

  async function choose(label: string) {
    await wrapper.get('.select-trigger').trigger('click')
    const option = Array.from(document.querySelectorAll<HTMLElement>('[role="option"]'))
      .reverse().find(item => item.textContent?.trim() === label)
    expect(option).toBeDefined()
    option!.click()
    await nextTick()
  }

  async function testAndNext() {
    await button('setup.status.testConnection').trigger('click')
    await flushPromises()
    expect(button('common.next').attributes('disabled')).toBeUndefined()
    await button('common.next').trigger('click')
  }

  it.each([
    ['sqlite', 'memory'], ['sqlite', 'redis'], ['postgres', 'memory'], ['postgres', 'redis']
  ])('submits %s + %s without changing port 8080', async (database, cache) => {
    expect(wrapper.get('input').element.value).toBe('data/sub2api.db')
    if (database === 'postgres') await choose('setup.database.typePostgres')
    await testAndNext()
    if (cache === 'redis') await choose('setup.redis.typeRedis')
    expect(wrapper.find('input[placeholder="data/cache-state.db"]').exists())
      .toBe(database === 'postgres' && cache === 'memory')
    await testAndNext()
    await wrapper.get('input[type="email"]').setValue('admin@example.com')
    await wrapper.findAll('input[type="password"]')[0].setValue('test-password-123')
    await wrapper.findAll('input[type="password"]')[1].setValue('different-password')
    expect(button('common.next').attributes('disabled')).toBeDefined()
    await wrapper.findAll('input[type="password"]')[1].setValue('test-password-123')
    await button('common.next').trigger('click')
    await button('setup.status.completeInstallation').trigger('click')
    await flushPromises()
    expect(install).toHaveBeenCalledWith(expect.objectContaining({
      database: expect.objectContaining({ type: database }),
      redis: expect.objectContaining({ type: cache }),
      server: { host: '0.0.0.0', port: 8080, mode: 'release' },
      admin: { email: 'admin@example.com', password: 'test-password-123' }
    }))
  })

  it('invalidates a successful database test when the path changes', async () => {
    await button('setup.status.testConnection').trigger('click')
    await flushPromises()
    await wrapper.get('input').setValue('data/another.db')
    expect(button('common.next').attributes('disabled')).toBeDefined()
    expect(testDatabase).toHaveBeenCalledWith(expect.objectContaining({ path: 'data/sub2api.db' }))
  })

  it('does not accept an old asynchronous database result', async () => {
    let resolve!: (value: { message: string }) => void
    vi.mocked(testDatabase).mockReturnValueOnce(new Promise(done => { resolve = done }))
    await button('setup.status.testConnection').trigger('click')
    await wrapper.get('input').setValue('data/another.db')
    resolve({ message: 'ok' })
    await flushPromises()
    expect(button('common.next').attributes('disabled')).toBeDefined()
    await testAndNext()
  })

  it('invalidates cache tests on capacity changes including pending responses', async () => {
    await testAndNext()
    let resolve!: (value: { message: string }) => void
    vi.mocked(testRedis).mockReturnValueOnce(new Promise(done => { resolve = done }))
    await button('setup.status.testConnection').trigger('click')
    await wrapper.get('input[placeholder="512"]').setValue(256)
    resolve({ message: 'ok' })
    await flushPromises()
    expect(button('common.next').attributes('disabled')).toBeDefined()
    expect(testRedis).toHaveBeenCalledWith(expect.objectContaining({
      memory: expect.objectContaining({ max_capacity_mb: 512 })
    }))
    await button('setup.status.testConnection').trigger('click')
    await flushPromises()
    await wrapper.get('input[placeholder="512"]').setValue(128)
    expect(button('common.next').attributes('disabled')).toBeDefined()
  })

  it('shows a connection error and permits retry', async () => {
    vi.mocked(testDatabase).mockRejectedValueOnce(new Error('database unavailable'))
    await button('setup.status.testConnection').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('database unavailable')
    expect(button('common.next').attributes('disabled')).toBeDefined()
    await testAndNext()
    expect(wrapper.text()).not.toContain('database unavailable')
  })
})
