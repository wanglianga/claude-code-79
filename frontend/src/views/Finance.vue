<template>
  <div>
    <div class="page-card">
      <div class="flex-row" style="justify-content: space-between">
        <h3 class="page-title" style="margin:0">财政核销（实际签收才发放补贴，防止补贴与服务脱节）</h3>
        <div class="flex-row">
          <el-date-picker v-model="month" type="month" value-format="YYYY-MM" placeholder="选择核销月份" />
          <el-button type="primary" :loading="acting" @click="generate">生成/刷新核销单</el-button>
        </div>
      </div>
    </div>

    <div class="page-card">
      <h3 class="page-title">核销单列表</h3>
      <el-table :data="list" v-loading="loading" @row-click="(r) => $router.push(`/finance/${r.id}`)" row-style="cursor:pointer">
        <el-table-column prop="month" label="核销月份" width="110" />
        <el-table-column label="状态" width="160">
          <template #default="{ row }">
            <el-tag :type="recStatus[row.status]?.type">{{ recStatus[row.status]?.text }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="total_orders" label="餐单总数" width="90" />
        <el-table-column prop="signed_orders" label="实际签收" width="90" />
        <el-table-column label="补贴发放(元)" width="120">
          <template #default="{ row }"><b class="danger-text">{{ fmtMoney(row.subsidy_total) }}</b></template>
        </el-table-column>
        <el-table-column label="退餐(元)" width="100">
          <template #default="{ row }">{{ fmtMoney(row.refund_total) }}</template>
        </el-table-column>
        <el-table-column label="厨房结算(元)" width="120">
          <template #default="{ row }">{{ fmtMoney(row.kitchen_settlement) }}</template>
        </el-table-column>
        <el-table-column label="餐盒回收率" width="100">
          <template #default="{ row }">{{ row.recycle_rate }}%</template>
        </el-table-column>
        <el-table-column prop="anomaly_count" label="异常(件)" width="90" />
        <el-table-column prop="followup_done" label="已回访" width="80" />
        <el-table-column prop="created_by" label="经办人" width="90" />
      </el-table>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'
import { recStatus, fmtMoney } from '../utils'

const router = useRouter()
const list = ref([])
const loading = ref(false)
const acting = ref(false)
const month = ref(new Date().toISOString().slice(0, 7))

async function load() {
  loading.value = true
  try {
    list.value = await api.get('/finance/reconciliations')
  } finally {
    loading.value = false
  }
}

async function generate() {
  if (!month.value) {
    ElMessage.warning('请选择核销月份')
    return
  }
  await ElMessageBox.confirm(
    `将汇总 ${month.value} 的实际签收餐单、补贴金额、退餐、异常回访与餐盒回收率，生成核销草稿。已归档月份不可重新生成。`,
    '生成核销单',
    { type: 'info' }
  )
  acting.value = true
  try {
    const res = await api.post('/finance/reconciliations', { month: month.value })
    ElMessage.success('核销单已生成')
    router.push(`/finance/${res.id}`)
  } finally {
    acting.value = false
  }
}

onMounted(load)
</script>
