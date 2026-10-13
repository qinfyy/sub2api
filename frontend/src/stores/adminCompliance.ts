import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import adminComplianceAPI, { type AdminComplianceStatus } from '@/api/admin/compliance'
import { getLocale } from '@/i18n'

const FALLBACK_ZH_PHRASE = '我已阅读、理解并同意 Sub2API 部署与运营合规承诺'
const FALLBACK_EN_PHRASE = 'I have read, understood, and agree to the Sub2API Deployment and Operation Compliance Commitment'

export const useAdminComplianceStore = defineStore('adminCompliance', () => {
  const status = ref<AdminComplianceStatus | null>(null)
  const loading = ref(false)
  const submitting = ref(false)
  const initialized = ref(false)
  const forceVisible = ref(false)
  let session = 0
  let revision = 0
  let pending: Promise<AdminComplianceStatus> | null = null
  let retryAt = 0
  let lastFailure: unknown

  const required = computed(() => status.value?.required === true)
  const shouldShow = computed(() => required.value || forceVisible.value)
  const currentLocale = computed(() => getLocale())
  const expectedPhrase = computed(() => {
    if (currentLocale.value === 'zh') {
      return status.value?.ack_phrase_zh || FALLBACK_ZH_PHRASE
    }
    return status.value?.ack_phrase_en || FALLBACK_EN_PHRASE
  })

  function fetchStatus(): Promise<AdminComplianceStatus> {
    if (pending) return pending
    if (Date.now() < retryAt) return Promise.reject(lastFailure)
    const requestSession = session
    const requestRevision = revision
    loading.value = true
    const request = adminComplianceAPI.getStatus().then((nextStatus) => {
      if (requestSession !== session) throw new Error('Compliance session changed')
      if (requestRevision === revision) {
        status.value = nextStatus
        initialized.value = true
        forceVisible.value = nextStatus.required
      }
      retryAt = 0
      lastFailure = undefined
      return nextStatus
    }).catch((error: unknown) => {
      if (requestSession === session && requestRevision === revision) {
        lastFailure = error
        retryAt = Date.now() + 5000
      }
      throw error
    }).finally(() => {
      if (pending === request) {
        pending = null
        loading.value = false
      }
    })
    pending = request
    return request
  }

  async function accept(phrase: string): Promise<AdminComplianceStatus> {
    const requestSession = session
    submitting.value = true
    try {
      const nextStatus = await adminComplianceAPI.accept({
        phrase,
        language: currentLocale.value
      })
      if (requestSession !== session) throw new Error('Compliance session changed')
      revision += 1
      retryAt = 0
      lastFailure = undefined
      status.value = nextStatus
      initialized.value = true
      forceVisible.value = nextStatus.required
      return nextStatus
    } finally {
      if (requestSession === session) submitting.value = false
    }
  }

  function requireAcknowledgement(partialStatus?: Partial<AdminComplianceStatus>): void {
    revision += 1
    status.value = {
      required: true,
      version: partialStatus?.version || status.value?.version || 'v2026.06.10',
      document_path_zh: partialStatus?.document_path_zh || status.value?.document_path_zh || 'docs/legal/admin-compliance.zh.md',
      document_path_en: partialStatus?.document_path_en || status.value?.document_path_en || 'docs/legal/admin-compliance.en.md',
      document_url_zh: partialStatus?.document_url_zh || status.value?.document_url_zh || 'https://github.com/Wei-Shaw/sub2api/blob/main/docs/legal/admin-compliance.zh.md',
      document_url_en: partialStatus?.document_url_en || status.value?.document_url_en || 'https://github.com/Wei-Shaw/sub2api/blob/main/docs/legal/admin-compliance.en.md',
      ack_phrase_zh: partialStatus?.ack_phrase_zh || status.value?.ack_phrase_zh || FALLBACK_ZH_PHRASE,
      ack_phrase_en: partialStatus?.ack_phrase_en || status.value?.ack_phrase_en || FALLBACK_EN_PHRASE,
      acknowledgement: status.value?.acknowledgement
    }
    initialized.value = true
    forceVisible.value = true
  }

  function reset(): void {
    session += 1
    revision += 1
    pending = null
    retryAt = 0
    lastFailure = undefined
    status.value = null
    loading.value = false
    submitting.value = false
    initialized.value = false
    forceVisible.value = false
  }

  return {
    status,
    loading,
    submitting,
    initialized,
    required,
    shouldShow,
    expectedPhrase,
    fetchStatus,
    accept,
    requireAcknowledgement,
    reset
  }
})
