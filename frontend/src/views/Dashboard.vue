<template>
  <div>
    <div class="page-card">
      <h3 class="page-title">{{ greeting }}，{{ store.user?.name }}（{{ roleNames[store.role] }}）</h3>
      <div class="muted">{{ todayText }} · 平台将老人、家属、社区、厨房、骑手与财政核销汇聚在同一餐单中协同处理</div>
    </div>

    <div class="stat-grid mb-12">
      <div v-for="s in statCards" :key="s.label" class="stat-card">
        <div class="label">{{ s.label }}</div>
        <div class="value" :class="{ plain: s.plain }">{{ s.value }}</div>
        <div class="muted" v-if="s.hint">{{ s.hint }}</div>
      </div>
    </div>

    <div class="page-card">
      <h3 class="page-title">今日餐单状态分布（各端状态实时一致）</h3>
      <div v-if="todayStats.length === 0" class="muted">今日暂无餐单</div>
      <div v-else class="flex-row">
        <el-tag v-for="t in todayStats" :key="t.status" :type="orderStatus[t.status]?.type || 'info'"
          size="large" effect="plain" style="font-size:14px; padding:14px 16px">
          {{ orderStatus[t.status]?.text || t.status }}：{{ t.count }} 单
        </el-tag>
      </div>
    </div>

    <div class="page-card">
      <h3 class="page-title">快捷入口</h3>
      <div class="flex-row">
        <el-button v-if="['family','elder','community','admin'].includes(store.role)" type="primary"
          @click="$router.push('/order/new')">立即订餐</el-button>
        <el-button @click="$router.push('/orders')">查看餐单</el-button>
        <el-button v-if="store.role === 'kitchen' || store.role === 'admin'" @click="$router.push('/kitchen')">厨房工作台</el-button>
        <el-button v-if="['rider','volunteer'].includes(store.role)" @click="$router.push('/delivery')">配送任务</el-button>
        <el-button @click="$router.push('/anomalies')">异常工单</el-button>
        <el-button v-if="['finance','admin'].includes(store.role)" @click="$router.push('/finance')">财政核销</el-button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { store, roleNames } from '../store'
import { orderStatus } from '../utils'
import api from '../api'

const stats = ref({})
const todayStats = ref([])

const greetings = ['您好']
const greeting = computed(() => greetings[0])
const todayText = new Date().toLocaleDateString('zh-CN', { year: 'numeric', month: 'long', day: 'numeric', weekday: 'long' })

const labels = {
  month_orders: '本月餐单（单）',
  month_subsidy: '本月已享补贴（元）',
  open_anomalies: '待处理异常（件）',
  pending_boxes: '待回收餐盒（个）',
  today_confirmed: '今日待备餐（单）',
  today_preparing: '今日备餐中（单）',
  today_ready: '今日已出餐（单）',
  unsuitable_feedback: '饭菜不适合反馈（件）',
  pool_tasks: '待接单任务（个）',
  my_delivering: '我配送中（单）',
  today_delivered: '今日已送达（单）',
  today_timeout: '今日超时（单）',
  processing_anomalies: '回访处理中（件）',
  high_priority: '高优先级工单（件）',
  today_pickup: '今日待现场取餐（单）',
  elders_total: '在册老人（人）',
  focus_elders: '今日重点关注（人）',
  high_risk_elders: '高风险老人（人）',
  month_signed: '本月实际签收（单）',
  month_refund: '本月退餐金额（元）',
  recycle_rate: '本月餐盒回收率（%）',
  archived_months: '已归档财政档案（份）'
}

const statCards = computed(() => {
  const out = []
  for (const [k, v] of Object.entries(stats.value)) {
    if (k === 'role' || k === 'today_by_status') continue
    out.push({ label: labels[k] || k, value: v, plain: k.includes('rate') })
  }
  return out
})

onMounted(async () => {
  const d = await api.get('/dashboard')
  stats.value = d
  todayStats.value = d.today_by_status || []
})
</script>
