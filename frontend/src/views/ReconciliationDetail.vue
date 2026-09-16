<template>
  <div v-loading="loading">
    <template v-if="r">
      <div class="page-card">
        <div class="flex-row" style="justify-content: space-between">
          <h3 class="page-title" style="margin:0">{{ r.month }} 核销单 #{{ r.id }}</h3>
          <div class="flex-row">
            <el-tag :type="recStatus[r.status]?.type" size="large">{{ recStatus[r.status]?.text }}</el-tag>
            <el-button v-if="r.status === 'draft'" type="warning" :loading="acting" @click="confirm">确认核销</el-button>
            <el-button v-if="r.status === 'confirmed'" type="success" :loading="acting" @click="archive">归档财政档案</el-button>
          </div>
        </div>
        <div class="stat-grid mt-12">
          <div class="stat-card"><div class="label">餐单总数</div><div class="value plain">{{ r.total_orders }}</div></div>
          <div class="stat-card"><div class="label">实际签收</div><div class="value">{{ r.signed_orders }}</div></div>
          <div class="stat-card"><div class="label">取消/退餐</div><div class="value plain">{{ r.cancelled_orders }}</div></div>
          <div class="stat-card"><div class="label">异常未结</div><div class="value plain">{{ r.exception_orders }}</div></div>
          <div class="stat-card"><div class="label">补贴发放</div><div class="value">{{ fmtMoney(r.subsidy_total) }}</div></div>
          <div class="stat-card"><div class="label">退餐金额</div><div class="value plain">{{ fmtMoney(r.refund_total) }}</div></div>
          <div class="stat-card"><div class="label">老人自付</div><div class="value plain">{{ fmtMoney(r.payable_total) }}</div></div>
          <div class="stat-card"><div class="label">厨房结算</div><div class="value">{{ fmtMoney(r.kitchen_settlement) }}</div></div>
          <div class="stat-card"><div class="label">异常工单</div><div class="value plain">{{ r.anomaly_count }}</div></div>
          <div class="stat-card"><div class="label">完成回访</div><div class="value plain">{{ r.followup_done }}</div></div>
          <div class="stat-card">
            <div class="label">餐盒回收率</div>
            <div class="value" :class="{ plain: r.recycle_rate >= 95 }">{{ r.recycle_rate }}%</div>
            <div class="muted">{{ r.boxes_returned }}/{{ r.boxes_issued }} 个</div>
          </div>
          <div class="stat-card"><div class="label">节日加餐补贴</div><div class="value plain">{{ fmtMoney(r.holiday_extra_total) }}</div></div>
        </div>
        <div class="muted mt-12">
          经办人：{{ r.created_by }} · 生成于 {{ fmtTime(r.created_at) }}
          <span v-if="r.confirmed_at"> · 确认于 {{ fmtTime(r.confirmed_at) }}</span>
          <span v-if="r.archived_at"> · 归档于 {{ fmtTime(r.archived_at) }}</span>
        </div>
      </div>

      <div class="page-card">
        <h3 class="page-title">核销明细（逐单判定补贴发放，未签收不发放）</h3>
        <el-table :data="r.items" size="small" max-height="560">
          <el-table-column prop="order_no" label="单号" width="140">
            <template #default="{ row }">
              <el-link type="primary" @click="$router.push(`/orders/${row.order_id}`)">{{ row.order_no }}</el-link>
            </template>
          </el-table-column>
          <el-table-column prop="elder_name" label="老人" width="90" />
          <el-table-column prop="meal_date" label="用餐日期" width="110" />
          <el-table-column label="餐单状态" width="130">
            <template #default="{ row }">
              <el-tag :type="orderStatus[row.order_status]?.type" size="small">
                {{ orderStatus[row.order_status]?.text || row.order_status }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="餐费" width="100">
            <template #default="{ row }">{{ fmtMoney(row.total_amount) }}</template>
          </el-table-column>
          <el-table-column label="补贴" width="100">
            <template #default="{ row }">{{ fmtMoney(row.subsidy_amount) }}</template>
          </el-table-column>
          <el-table-column label="是否发放" width="90">
            <template #default="{ row }">
              <el-tag :type="row.included ? 'success' : 'info'" size="small">{{ row.included ? '发放' : '不发放' }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="真实签收依据" width="150">
            <template #default="{ row }">
              <el-tag v-if="row.sign_basis" :type="(signBasisMap[row.sign_basis]||{}).type||'info'" size="small">
                {{ row.sign_basis_name || signBasisMap[row.sign_basis]?.text }}
              </el-tag>
              <span v-else class="muted">—</span>
            </template>
          </el-table-column>
          <el-table-column prop="reason" label="判定依据" min-width="180" />
        </el-table>
      </div>
    </template>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'
import { recStatus, orderStatus, signBasisMap, fmtTime, fmtMoney } from '../utils'

const route = useRoute()
const router = useRouter()
const r = ref(null)
const loading = ref(false)
const acting = ref(false)

async function load() {
  loading.value = true
  try {
    r.value = await api.get(`/finance/reconciliations/${route.params.id}`)
  } finally {
    loading.value = false
  }
}

async function confirm() {
  await ElMessageBox.confirm('确认后核销数据将锁定，归档前请核对补贴发放与退餐明细。确认核销？', '确认核销', { type: 'warning' })
  acting.value = true
  try {
    await api.post(`/finance/reconciliations/${r.value.id}/confirm`)
    ElMessage.success('已确认')
    await load()
  } finally {
    acting.value = false
  }
}

async function archive() {
  await ElMessageBox.confirm(
    '归档后财政档案不可修改，当月已签收餐单将标记为「已核销」。确认归档？',
    '归档财政档案',
    { type: 'warning', confirmButtonText: '确认归档', cancelButtonText: '再核对一下' }
  )
  acting.value = true
  try {
    await api.post(`/finance/reconciliations/${r.value.id}/archive`)
    ElMessage.success('已归档，财政档案生成')
    router.push('/finance')
  } finally {
    acting.value = false
  }
}

onMounted(load)
</script>
