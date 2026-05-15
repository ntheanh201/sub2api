<template>
  <section class="space-y-4">
    <!-- Filters -->
    <div class="card flex flex-wrap items-center gap-3 p-4">
      <div class="flex items-center gap-2">
        <label class="text-xs font-medium text-gray-500">Time Range</label>
        <select
          v-model="rangePreset"
          class="rounded-md border border-gray-200 bg-white px-3 py-1.5 text-sm dark:border-gray-700 dark:bg-gray-800"
          @change="onPresetChange"
        >
          <option value="today">Today</option>
          <option value="7d">Last 7 Days</option>
          <option value="30d">Last 30 Days</option>
          <option value="custom">Custom</option>
        </select>
      </div>

      <template v-if="rangePreset === 'custom'">
        <input
          v-model="startDate"
          type="date"
          class="rounded-md border border-gray-200 bg-white px-2 py-1.5 text-sm dark:border-gray-700 dark:bg-gray-800"
          @change="reload"
        />
        <span class="text-gray-400">→</span>
        <input
          v-model="endDate"
          type="date"
          class="rounded-md border border-gray-200 bg-white px-2 py-1.5 text-sm dark:border-gray-700 dark:bg-gray-800"
          @change="reload"
        />
      </template>

      <div class="ml-auto flex items-center gap-2">
        <input
          v-model="search"
          type="text"
          placeholder="Search key name…"
          class="w-48 rounded-md border border-gray-200 bg-white px-3 py-1.5 text-sm dark:border-gray-700 dark:bg-gray-800"
        />
        <button
          class="rounded-md border border-gray-200 px-3 py-1.5 text-sm hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-800"
          :disabled="loading"
          @click="reload"
        >
          Refresh
        </button>
      </div>
    </div>

    <!-- Hero Cards -->
    <div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
      <div class="card flex items-center gap-3 p-4">
        <div class="rounded-lg bg-blue-100 p-2 text-blue-600 dark:bg-blue-900/30">
          <Icon name="document" size="md" />
        </div>
        <div>
          <p class="text-xs font-medium text-gray-500">Total Requests</p>
          <p class="text-xl font-bold">{{ summary.total_requests.toLocaleString() }}</p>
          <p class="text-xs text-gray-400">in selected range</p>
        </div>
      </div>

      <div class="card flex items-center gap-3 p-4">
        <div class="rounded-lg bg-amber-100 p-2 text-amber-600 dark:bg-amber-900/30">
          <Icon name="cube" size="md" />
        </div>
        <div>
          <p class="text-xs font-medium text-gray-500">Total Tokens</p>
          <p class="text-xl font-bold">{{ formatTokens(summary.total_tokens) }}</p>
          <p class="text-xs text-gray-500">
            In: {{ formatTokens(summary.total_input_tokens) }} / Out: {{ formatTokens(summary.total_output_tokens) }}
          </p>
        </div>
      </div>

      <div class="card flex items-center gap-3 p-4">
        <div class="rounded-lg bg-green-100 p-2 text-green-600 dark:bg-green-900/30">
          <Icon name="dollar" size="md" />
        </div>
        <div class="min-w-0 flex-1">
          <p class="text-xs font-medium text-gray-500">Total Cost</p>
          <p class="text-xl font-bold text-green-600">${{ summary.total_actual_cost.toFixed(4) }}</p>
          <p class="text-xs text-gray-400">
            Actual /
            <span class="line-through">${{ summary.total_cost.toFixed(4) }}</span>
            Standard
          </p>
        </div>
      </div>

      <div class="card flex items-center gap-3 p-4">
        <div class="rounded-lg bg-purple-100 p-2 text-purple-600 dark:bg-purple-900/30">
          <Icon name="clock" size="md" />
        </div>
        <div>
          <p class="text-xs font-medium text-gray-500">Avg Duration</p>
          <p class="text-xl font-bold">{{ formatDuration(summary.average_duration_ms) }}</p>
          <p class="text-xs text-gray-400">per request</p>
        </div>
      </div>
    </div>

    <!-- Leaderboard Table -->
    <div class="card overflow-hidden">
      <div class="flex items-center justify-between border-b border-gray-100 px-4 py-3 dark:border-gray-800">
        <h3 class="text-base font-semibold">API Key Token Legend</h3>
        <span class="text-xs text-gray-500">{{ filteredKeys.length }} keys</span>
      </div>
      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead class="bg-gray-50 text-xs uppercase text-gray-500 dark:bg-gray-900/40">
            <tr>
              <th class="px-3 py-2 text-left">#</th>
              <th class="px-3 py-2 text-left">API Key</th>
              <th v-if="admin" class="px-3 py-2 text-left">Owner</th>
              <th
                class="cursor-pointer px-3 py-2 text-right hover:text-gray-700 dark:hover:text-gray-300"
                @click="setSort('requests')"
              >
                Requests {{ sortArrow('requests') }}
              </th>
              <th
                class="cursor-pointer px-3 py-2 text-right hover:text-gray-700 dark:hover:text-gray-300"
                @click="setSort('total_tokens')"
              >
                Tokens {{ sortArrow('total_tokens') }}
              </th>
              <th
                class="cursor-pointer px-3 py-2 text-right hover:text-gray-700 dark:hover:text-gray-300"
                @click="setSort('actual_cost')"
              >
                Cost {{ sortArrow('actual_cost') }}
              </th>
              <th class="px-3 py-2 text-right">Cache Hit</th>
              <th
                class="cursor-pointer px-3 py-2 text-right hover:text-gray-700 dark:hover:text-gray-300"
                @click="setSort('average_duration_ms')"
              >
                Avg {{ sortArrow('average_duration_ms') }}
              </th>
              <th class="px-3 py-2 text-right">Last Used</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td :colspan="admin ? 9 : 8" class="px-3 py-8 text-center text-gray-400">Loading…</td>
            </tr>
            <tr v-else-if="filteredKeys.length === 0">
              <td :colspan="admin ? 9 : 8" class="px-3 py-8 text-center text-gray-400">No API keys to show</td>
            </tr>
            <tr
              v-for="(row, idx) in filteredKeys"
              v-else
              :key="row.api_key_id"
              class="border-t border-gray-100 hover:bg-gray-50 dark:border-gray-800 dark:hover:bg-gray-900/40"
            >
              <td class="px-3 py-2.5">
                <span :class="rankClass(idx)">{{ rankLabel(idx) }}</span>
              </td>
              <td class="px-3 py-2.5">
                <div class="flex items-center gap-2">
                  <span
                    class="inline-block h-2 w-2 rounded-full"
                    :class="statusDotClass(row.status)"
                    :title="row.status"
                  ></span>
                  <span class="font-medium">{{ row.name }}</span>
                </div>
              </td>
              <td v-if="admin" class="px-3 py-2.5 text-xs text-gray-600 dark:text-gray-400">
                {{ row.user_email || '—' }}
              </td>
              <td class="px-3 py-2.5 text-right tabular-nums">{{ row.requests.toLocaleString() }}</td>
              <td class="px-3 py-2.5 text-right tabular-nums">
                {{ formatTokens(row.total_tokens) }}
              </td>
              <td class="px-3 py-2.5 text-right tabular-nums">
                <span :class="costClass(row.actual_cost)">${{ row.actual_cost.toFixed(4) }}</span>
              </td>
              <td class="px-3 py-2.5 text-right">
                <div v-if="row.requests > 0" class="flex items-center justify-end gap-2">
                  <div class="h-1.5 w-16 overflow-hidden rounded-full bg-gray-100 dark:bg-gray-800">
                    <div
                      class="h-full"
                      :class="cacheBarClass(row.cache_hit_pct)"
                      :style="{ width: `${Math.min(row.cache_hit_pct, 100)}%` }"
                    ></div>
                  </div>
                  <span class="w-10 tabular-nums text-xs text-gray-500">{{ row.cache_hit_pct.toFixed(0) }}%</span>
                </div>
                <span v-else class="text-xs text-gray-400">—</span>
              </td>
              <td class="px-3 py-2.5 text-right tabular-nums">
                {{ row.requests > 0 ? formatDuration(row.average_duration_ms) : '—' }}
              </td>
              <td class="px-3 py-2.5 text-right text-xs text-gray-500">
                {{ formatRelative(row.last_used_at) }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { apiClient } from '@/api/client'
import Icon from '@/components/icons/Icon.vue'

const props = withDefaults(
  defineProps<{
    /** When true, hits the admin endpoint and shows an Owner column. */
    admin?: boolean
  }>(),
  { admin: false }
)

const endpoint = props.admin
  ? '/admin/dashboard/api-keys-leaderboard'
  : '/usage/dashboard/api-keys-leaderboard'

interface LeaderboardRow {
  api_key_id: number
  name: string
  status: string
  last_used_at?: string | null
  user_id?: number
  user_email?: string
  requests: number
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  total_tokens: number
  total_cost: number
  actual_cost: number
  average_duration_ms: number
  cache_hit_pct: number
}

interface Summary {
  total_requests: number
  total_input_tokens: number
  total_output_tokens: number
  total_cache_tokens: number
  total_tokens: number
  total_cost: number
  total_actual_cost: number
  average_duration_ms: number
}

interface LeaderboardResponse {
  summary: Summary
  keys: LeaderboardRow[]
}

const emptySummary: Summary = {
  total_requests: 0,
  total_input_tokens: 0,
  total_output_tokens: 0,
  total_cache_tokens: 0,
  total_tokens: 0,
  total_cost: 0,
  total_actual_cost: 0,
  average_duration_ms: 0,
}

const loading = ref(false)
const keys = ref<LeaderboardRow[]>([])
const summary = ref<Summary>({ ...emptySummary })
const search = ref('')
const rangePreset = ref<'today' | '7d' | '30d' | 'custom'>('7d')
const sortKey = ref<keyof LeaderboardRow>('actual_cost')
const sortOrder = ref<'asc' | 'desc'>('desc')

const toIso = (d: Date) => d.toISOString().split('T')[0]
const startDate = ref(toIso(new Date(Date.now() - 6 * 86400000)))
const endDate = ref(toIso(new Date()))

const onPresetChange = () => {
  const now = new Date()
  if (rangePreset.value === 'today') {
    startDate.value = toIso(now)
    endDate.value = toIso(now)
  } else if (rangePreset.value === '7d') {
    startDate.value = toIso(new Date(now.getTime() - 6 * 86400000))
    endDate.value = toIso(now)
  } else if (rangePreset.value === '30d') {
    startDate.value = toIso(new Date(now.getTime() - 29 * 86400000))
    endDate.value = toIso(now)
  }
  if (rangePreset.value !== 'custom') reload()
}

const reload = async () => {
  loading.value = true
  try {
    const { data } = await apiClient.get<LeaderboardResponse>(endpoint, {
      params: { start_date: startDate.value, end_date: endDate.value },
    })
    keys.value = data.keys || []
    summary.value = data.summary || { ...emptySummary }
  } catch (err) {
    console.error('Failed to load leaderboard:', err)
    keys.value = []
    summary.value = { ...emptySummary }
  } finally {
    loading.value = false
  }
}

const filteredKeys = computed(() => {
  const q = search.value.trim().toLowerCase()
  let rows = q
    ? keys.value.filter(
        (r) =>
          r.name.toLowerCase().includes(q) ||
          (r.user_email || '').toLowerCase().includes(q)
      )
    : keys.value.slice()
  const k = sortKey.value
  const dir = sortOrder.value === 'desc' ? -1 : 1
  rows.sort((a, b) => {
    const av = (a[k] ?? 0) as number
    const bv = (b[k] ?? 0) as number
    return av === bv ? 0 : av < bv ? -1 * dir : 1 * dir
  })
  return rows
})

const setSort = (key: keyof LeaderboardRow) => {
  if (sortKey.value === key) {
    sortOrder.value = sortOrder.value === 'desc' ? 'asc' : 'desc'
  } else {
    sortKey.value = key
    sortOrder.value = 'desc'
  }
}

const sortArrow = (key: keyof LeaderboardRow) =>
  sortKey.value !== key ? '' : sortOrder.value === 'desc' ? '↓' : '↑'

const rankLabel = (idx: number) => (idx === 0 ? '🥇' : idx === 1 ? '🥈' : idx === 2 ? '🥉' : `${idx + 1}`)
const rankClass = (idx: number) => (idx < 3 ? 'text-base' : 'text-xs text-gray-400 tabular-nums')

const statusDotClass = (status: string) => {
  if (status === 'active') return 'bg-emerald-500'
  if (status === 'disabled') return 'bg-gray-400'
  return 'bg-amber-500'
}

const costClass = (cost: number) => {
  if (cost > 200) return 'text-red-600 font-semibold'
  if (cost > 50) return 'text-amber-600'
  return 'text-gray-700 dark:text-gray-300'
}

const cacheBarClass = (pct: number) => {
  if (pct < 20) return 'bg-red-400'
  if (pct < 50) return 'bg-amber-400'
  return 'bg-emerald-500'
}

const formatTokens = (n: number) => {
  if (n >= 1e9) return (n / 1e9).toFixed(2) + 'B'
  if (n >= 1e6) return (n / 1e6).toFixed(2) + 'M'
  if (n >= 1e3) return (n / 1e3).toFixed(2) + 'K'
  return n.toLocaleString()
}
const formatDuration = (ms: number) =>
  ms < 1000 ? `${ms.toFixed(0)}ms` : `${(ms / 1000).toFixed(2)}s`

const formatRelative = (iso?: string | null) => {
  if (!iso) return 'never'
  const diff = (Date.now() - new Date(iso).getTime()) / 1000
  if (diff < 60) return 'just now'
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`
  return `${Math.floor(diff / 86400)}d ago`
}

onMounted(reload)
</script>
