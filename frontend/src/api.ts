export const API = (import.meta.env.VITE_API_URL as string | undefined) ?? 'http://localhost:8080'

export type JobStatus =
  | 'QUEUED' | 'PROCESSING' | 'VERIFIED' | 'COVERAGE_GAP_FLAGGED'
  | 'RETRYING' | 'NEEDS_MANUAL_REVIEW' | 'MANUAL_RESOLVED' | 'CALL_IN_PROGRESS' | 'EMAIL_PENDING'

export type ReviewReason = 'unsupported_payer' | 'ambiguous_match' | 'retry_exhausted' | 'malformed_response' | 'payer_rejected' | 'voice_call_failed' | 'call_timeout' | 'email_not_answered' | 'email_response_invalid'

export type VerificationSource = 'stedi_270_271' | 'ai_voice_call' | 'email_form'

export interface Payer {
  id: string; name: string; stediPayerId: string; supportsRealtime: boolean; serviceTypeCode: string; planType: string
  providerServicesPhone?: string; ivrNotes?: string; providerServicesEmail?: string
}

export interface Patient {
  id: string; practiceId: string; name: string; dob: string; memberId: string; payerId: string; createdAt: string
  payerName: string; stediPayerId: string; lastVerified?: string; lastStatus?: string; email?: string; phone?: string
}

export interface Category {
  stc: string; label: string; planPaysPct: number | null; patientCoinsurancePct: number | null; network?: string; covered: boolean; note?: string
}

export interface Facts {
  eligibilityStatus: 'active' | 'inactive' | 'unknown'; statusCode?: string; planName?: string; payerName?: string
  insuranceType?: string; planStart?: string; planEnd?: string
  deductibleAnnual: number | null; deductibleRemaining: number | null; annualMaximum: number | null; copayOffice: number | null
  categories: Category[]; nonCoveredLabels: string[]; limitations: string[]; flags: string[]; payerMessages: string[]; hasGap: boolean
}

export interface Brief {
  status: string; deductibleRemaining: number | null; coveragePercentByCategory: Record<string, number | null>
  flags: string[]; brief: string; summary: string; source: string; validation: string; model?: string; facts?: Facts
}

export interface Job {
  id: string; batchId?: string; patientId: string; payerId: string; status: JobStatus; attemptCount: number
  nextAttemptAt?: string; rawResponse?: unknown; normalizedBrief?: Brief; reviewReason?: ReviewReason
  errorCode?: string; errorMessage?: string; resolvedBy?: string; resolutionNote?: string
  startedAt?: string; completedAt?: string; resolvedAt?: string; createdAt: string; updatedAt: string
  verificationSource: VerificationSource; callId?: string; callTranscript?: string; callStartedAt?: string; callCompletedAt?: string
  emailToken?: string; emailSentAt?: string
  patientName: string; patientDob: string; memberId: string; payerName: string; stediPayerId: string; payerPhone?: string; payerEmail?: string
}

export interface JobAttempt {
  id: string; jobId: string; attemptNumber: number; status: string; errorCode?: string; errorMessage?: string; attemptedAt: string
}

export interface Batch {
  id: string; practiceId: string; label: string; kind: string; totalJobs: number; queued: number; processing: number
  verified: number; gapFlagged: number; retrying: number; needsReview: number; manualResolved: number; createdAt: string; updatedAt: string
}

export interface Stats {
  total: number; verified: number; gapFlagged: number; inProgress: number; needsReview: number; manualResolved: number
  successRate: number; avgProcessingSeconds: number; byStatus: Record<string, number>
  byPayer: { payer: string; count: number }[]; last7Days: { day: string; count: number }[]; reviewReasons: Record<string, number>
}

export interface RateLimitState {
  payer: string; rps: number; burst: number; waiting: number; inFlight: number; throttledTotal: number; throttlingNow: boolean
}

export interface ConfigInfo {
  practice: { id: string; name: string }; stediMode: string; stediBaseURL: string; providerNPI: string; providerName: string
  llmEnabled: boolean; llmModel: string; maxWorkers: number; maxAttempts: number; retryBase: string; payerRPS: number; payerBurst: number
  emailDelivery?: boolean; emailProvider?: string; nightlyHour?: number; timezone?: string; practicePhone?: string; chatEnabled?: boolean
  voiceMode?: 'live' | 'mock'; voiceProvider?: 'bolna' | 'retell'; voiceCallTimeout?: string
  emailFormEnabled?: boolean; emailFormBaseURL?: string; emailResponseTimeout?: string
}


export interface FeeItem { id: string; practiceId: string; code: string; description: string; feeCents: number }

export interface Appointment {
  id: string; practiceId: string; patientId: string; scheduledAt: string; procedureCodes: string[]; notes?: string; createdAt: string
  patientName: string; patientEmail?: string; payerName: string; payerId: string; memberId: string
  jobId?: string; jobStatus?: JobStatus; noticeId?: string; noticeStatus?: string; reminderId?: string; reminderStatus?: string
}

export interface EstimateLine {
  code: string; description: string; category: string; feeCents: number; covered: boolean; planPaysPct: number | null
  deductibleAppliedCents: number; coinsuranceCents: number; overAnnualMaxCents: number; insurancePaysCents: number; patientPaysCents: number; note?: string
}

export interface Estimate {
  eligibilityStatus: string; planName?: string; totalFeeCents: number; insurancePaysCents: number; patientPaysCents: number
  deductibleRemainingStartCents: number | null; deductibleUsedCents: number; annualMaximumCents: number | null
  lines: EstimateLine[]; flags: string[]; disclaimer: string
}

export type NoticeStatus = 'logged' | 'sent' | 'failed' | 'skipped_no_recipient' | 'skipped_no_provider'

export type NoticeKind = 'cost_estimate' | 'reminder'

export interface PatientNotice {
  id: string; jobId: string; appointmentId?: string; patientId: string; kind: NoticeKind; channel: string; recipient?: string
  subject: string; bodyText: string; bodyHtml: string; estimate: Estimate; deliveryStatus: NoticeStatus
  deliveryError?: string; providerId?: string; createdAt: string; sentAt?: string
  patientName: string; payerName: string; scheduledAt?: string; jobStatus: JobStatus
}

export const NOTICE_META: Record<NoticeStatus, { label: string; tone: 'neutral' | 'info' | 'success' | 'warning' | 'error' | 'violet' }> = {
  logged: { label: 'Ready', tone: 'info' },
  sent: { label: 'Sent', tone: 'success' },
  failed: { label: 'Delivery failed', tone: 'error' },
  skipped_no_recipient: { label: 'No email on file', tone: 'warning' },
  skipped_no_provider: { label: 'Ready (delivery off)', tone: 'info' },
}

export const money = (cents: number) => `$${(cents / 100).toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const r = await fetch(API + path, { headers: { 'Content-Type': 'application/json' }, ...init })
  const text = await r.text()
  let body: unknown = null
  try { body = text ? JSON.parse(text) : null } catch { /* non-JSON */ }
  if (!r.ok) {
    const msg = (body as { error?: string } | null)?.error ?? `${r.status} ${r.statusText}`
    throw new Error(msg)
  }
  return body as T
}

export const api = {
  config: () => req<ConfigInfo>('/api/config'),
  stats: () => req<Stats>('/api/stats'),
  payers: () => req<Payer[]>('/api/payers'),
  patients: (search = '') => req<Patient[]>(`/api/patients?search=${encodeURIComponent(search)}&limit=500`),
  createPatient: (p: { name: string; dob: string; memberId: string; payerId: string; email?: string; phone?: string }) =>
    req<Patient>('/api/patients', { method: 'POST', body: JSON.stringify(p) }),
  verify: (body: Record<string, unknown>) =>
    req<{ jobId: string; job: Job }>('/api/verifications', { method: 'POST', body: JSON.stringify(body) }),
  jobs: (q: { status?: string; search?: string; batchId?: string; payerId?: string; limit?: number; offset?: number } = {}) => {
    const p = new URLSearchParams()
    Object.entries(q).forEach(([k, v]) => { if (v !== undefined && v !== '') p.set(k, String(v)) })
    return req<{ jobs: Job[]; total: number }>(`/api/verifications?${p}`)
  },
  job: (id: string) => req<{ job: Job; attempts: JobAttempt[] }>(`/api/verifications/${id}`),
  batches: (limit = 20) => req<Batch[]>(`/api/batches?limit=${limit}`),
  batch: (id: string, status = '', limit = 100) => req<{ batch: Batch; jobs: Job[]; total: number }>(`/api/batches/${id}?status=${status}&limit=${limit}`),
  createBatch: (body: { patientIds?: string[]; all?: boolean; label?: string }) =>
    req<Batch>('/api/batches', { method: 'POST', body: JSON.stringify(body) }),
  synthetic: (count: number) => req<Batch>('/api/batches/synthetic', { method: 'POST', body: JSON.stringify({ count }) }),
  uploadCSV: async (file: File) => {
    const fd = new FormData(); fd.append('file', file)
    const r = await fetch(API + '/api/batches/csv', { method: 'POST', body: fd })
    const body = await r.json()
    if (!r.ok) throw new Error(body.error ?? 'upload failed')
    return body as { batch: Batch; skipped: string[] }
  },
  review: () => req<{ jobs: Job[]; total: number }>('/api/review'),
  resolve: (id: string, body: { outcome: 'verified' | 'requeue'; resolvedBy: string; note: string }) =>
    req<Job>(`/api/review/${id}/resolve`, { method: 'POST', body: JSON.stringify(body) }),
  rateLimits: () => req<RateLimitState[]>('/api/ratelimits'),
  // pre-visit
  fees: () => req<FeeItem[]>('/api/fees'),
  upsertFee: (body: { code: string; description: string; feeCents: number }) => req<{ ok: boolean }>('/api/fees', { method: 'POST', body: JSON.stringify(body) }),
  appointments: (date = 'tomorrow', range = '') => req<{ appointments: Appointment[]; from: string; to: string }>(`/api/appointments?date=${date}${range ? `&range=${range}` : ''}`),
  createAppointment: (body: { patientId: string; scheduledAt: string; procedureCodes: string[]; notes?: string; autoRun?: boolean }) =>
    req<{ appointment: Appointment; queued: boolean }>('/api/appointments', { method: 'POST', body: JSON.stringify(body) }),
  deleteAppointment: (id: string) => req<{ ok: boolean }>(`/api/appointments/${id}`, { method: 'DELETE' }),
  verifyAppointment: (id: string) => req<{ batchId: string; queued: number }>(`/api/appointments/${id}/verify`, { method: 'POST' }),
  runPrevisit: (date = 'tomorrow') => req<{ queued: number; reminders: number; batch?: Batch | null; message?: string; day?: string }>(`/api/previsit/run?date=${date}`, { method: 'POST' }),
  notices: (limit = 100) => req<{ notices: PatientNotice[]; stats: Record<string, number> }>(`/api/notices?limit=${limit}`),
  notice: (id: string) => req<PatientNotice>(`/api/notices/${id}`),
  noticePreviewUrl: (id: string) => `${API}/api/notices/${id}/preview`,
  sendNotice: (id: string) => req<PatientNotice>(`/api/notices/${id}/send`, { method: 'POST' }),
  estimateForJob: (jobId: string, codes: string[]) => req<Estimate>(`/api/verifications/${jobId}/estimate?codes=${encodeURIComponent(codes.join(','))}`),
  updateContact: (patientId: string, body: { email: string; phone: string }) => req<{ ok: boolean }>(`/api/patients/${patientId}/contact`, { method: 'POST', body: JSON.stringify(body) }),
  deleteJobs: (ids: string[]) => req<{ deleted: number }>('/api/verifications', { method: 'DELETE', body: JSON.stringify({ ids }) }),
  deletePatients: (ids: string[]) => req<{ deleted: number }>('/api/patients', { method: 'DELETE', body: JSON.stringify({ ids }) }),
  deleteNotices: (ids: string[]) => req<{ deleted: number }>('/api/notices', { method: 'DELETE', body: JSON.stringify({ ids }) }),
  chat: (messages: ChatMessage[]) => req<ChatReply>('/api/chat', { method: 'POST', body: JSON.stringify({ messages }) }),
  queueStatus: () => req<QueueStatus>('/api/queue/status'),
  queuePause: () => req<QueueStatus>('/api/queue/pause', { method: 'POST' }),
  queueResume: () => req<QueueStatus>('/api/queue/resume', { method: 'POST' }),
  queuePurge: () => req<{ purged: number }>('/api/queue/purge', { method: 'POST' }),
  notifications: () => req<Notification[]>('/api/notifications'),
  updatePayerVoice: (id: string, body: { providerServicesPhone: string; ivrNotes: string }) => req<Payer>(`/api/payers/${id}/voice`, { method: 'POST', body: JSON.stringify(body) }),
  updatePayerEmail: (id: string, providerServicesEmail: string) => req<Payer>(`/api/payers/${id}/email`, { method: 'POST', body: JSON.stringify({ providerServicesEmail }) }),
  patientPdfUrl: (id: string) => `${API}/api/patients/${id}/pdf`,
  reportPdfUrl: () => `${API}/api/reports/pdf`,
}

export interface QueueStatus { paused: boolean; pausedAt?: string }

export interface Notification { id: string; kind: string; title: string; detail: string; createdAt: string; link: string }

export interface ChatMessage { role: 'user' | 'assistant'; text: string }
export interface ChatReply { text: string; toolsUsed?: string[] }

export const STATUS_META: Record<JobStatus, { label: string; tone: 'neutral' | 'info' | 'success' | 'warning' | 'error' | 'violet' }> = {
  QUEUED: { label: 'Queued', tone: 'neutral' },
  PROCESSING: { label: 'In Progress', tone: 'info' },
  VERIFIED: { label: 'Verified', tone: 'success' },
  COVERAGE_GAP_FLAGGED: { label: 'Coverage Gap', tone: 'warning' },
  RETRYING: { label: 'Retrying', tone: 'violet' },
  NEEDS_MANUAL_REVIEW: { label: 'Needs Review', tone: 'error' },
  MANUAL_RESOLVED: { label: 'Manually Verified', tone: 'success' },
  CALL_IN_PROGRESS: { label: 'Calling Payer', tone: 'violet' },
  EMAIL_PENDING: { label: 'Awaiting Email Response', tone: 'violet' },
}

export const REASON_TEXT: Record<ReviewReason, string> = {
  unsupported_payer: 'Payer does not support electronic eligibility checks',
  ambiguous_match: 'Payer could not identify the subscriber',
  retry_exhausted: 'Payer unavailable after all retry attempts',
  malformed_response: 'Payer response could not be parsed',
  payer_rejected: 'Payer rejected the request',
  voice_call_failed: 'AI phone call could not complete the verification',
  call_timeout: 'AI phone call timed out without a result',
  email_not_answered: 'Verification email was not answered in time',
  email_response_invalid: 'The submitted verification form had invalid or incomplete data',
}

export function fmtDate(s?: string) {
  if (!s) return '—'
  const d = new Date(s)
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
}
export function fmtTime(s?: string) {
  if (!s) return '—'
  return new Date(s).toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' })
}
export function fmtDOB(s: string) {
  return new Date(s).toLocaleDateString('en-US', { month: '2-digit', day: '2-digit', year: 'numeric', timeZone: 'UTC' })
}
