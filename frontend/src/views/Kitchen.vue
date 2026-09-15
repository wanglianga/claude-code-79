<template>
  <div>
    <div class="page-card">
      <div class="flex-row" style="justify-content: space-between">
        <h3 class="page-title" style="margin:0">厨房工作台</h3>
        <div class="flex-row">
          <el-date-picker v-model="date" type="date" value-format="YYYY-MM-DD" @change="load" />
          <el-radio-group v-model="mealType" @change="load">
            <el-radio-button value="lunch">午餐</el-radio-button>
            <el-radio-button value="dinner">晚餐</el-radio-button>
          </el-radio-group>
        </div>
      </div>
    </div>

    <el-row :gutter="16">
      <el-col :span="10">
        <div class="page-card">
          <h3 class="page-title">备餐汇总（按菜品）</h3>
          <el-table :data="summary.dishes" size="small">
            <el-table-column prop="dish_name" label="菜品" width="120" />
            <el-table-column label="营养要求" min-width="150">
              <template #default="{ row }">
                <el-tag v-if="row.low_salt" size="small" type="success" effect="plain">低盐</el-tag>
                <el-tag v-if="row.low_sugar" size="small" type="success" effect="plain" style="margin-left:4px">低糖</el-tag>
                <el-tag size="small" effect="plain" style="margin-left:4px">
                  {{ { normal: '普通', soft: '软', mushy: '软烂' }[row.softness] }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="qty" label="份数" width="70" />
            <el-table-column prop="custom_notes" label="个性化要求" min-width="130">
              <template #default="{ row }">
                <span class="warn-text">{{ row.custom_notes || '—' }}</span>
              </template>
            </el-table-column>
          </el-table>
          <div v-if="summary.dishes && summary.dishes.length === 0" class="muted">暂无需备餐的菜品</div>
        </div>

        <div class="page-card">
          <h3 class="page-title">出餐批次</h3>
          <div v-if="!summary.batch_id">
            <el-alert type="info" :closable="false" title="该日期餐别尚未创建批次" class="mb-12" />
            <el-button type="primary" :disabled="!summary.orders || summary.orders.filter(o => o.status === 'confirmed').length === 0"
              :loading="acting" @click="createBatch">
              创建批次并开始备餐（{{ (summary.orders || []).filter(o => o.status === 'confirmed').length }} 单）
            </el-button>
          </div>
          <template v-else>
            <div class="flex-row mb-12">
              <el-tag :type="summary.batch_status === 'released' ? 'success' : 'warning'" size="large">
                批次 #{{ summary.batch_id }} {{ summary.batch_status === 'released' ? '已出餐' : '备餐中' }}
              </el-tag>
            </div>
            <template v-if="summary.batch_status !== 'released'">
              <el-alert type="warning" :closable="false" class="mb-12"
                title="出餐时请录入每道菜的实际出餐数量；少于计划数量将自动生成「厨房少做」异常工单" />
              <el-table :data="batchDetail.items" size="small" class="mb-12">
                <el-table-column prop="dish_name" label="菜品" />
                <el-table-column prop="planned_qty" label="计划" width="70" />
                <el-table-column label="实际出餐" width="150">
                  <template #default="{ row }">
                    <el-input-number v-model="row.actual_qty" :min="0" :max="row.planned_qty + 10" size="small" />
                  </template>
                </el-table-column>
              </el-table>
              <el-button type="primary" :loading="acting" @click="releaseBatch">完成出餐，通知配送</el-button>
            </template>
          </template>
        </div>
      </el-col>

      <el-col :span="14">
        <div class="page-card">
          <h3 class="page-title">餐单明细（{{ (summary.orders || []).length }} 单）</h3>
          <el-table :data="summary.orders" size="small" @row-click="(r) => $router.push(`/orders/${r.id}`)"
            row-style="cursor:pointer">
            <el-table-column prop="order_no" label="单号" width="140" />
            <el-table-column prop="elder_name" label="老人" width="90" />
            <el-table-column label="菜品" min-width="200">
              <template #default="{ row }">
                <div v-for="(it, i) in row.items" :key="i">
                  {{ it.dish_name }}×{{ it.qty }}
                  <span v-if="it.custom_note" class="warn-text">（{{ it.custom_note }}）</span>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="饮食禁忌" min-width="110">
              <template #default="{ row }">
                <span class="danger-text">{{ row.dietary_restrictions || '—' }}</span>
              </template>
            </el-table-column>
            <el-table-column label="状态" width="130">
              <template #default="{ row }">
                <el-tag :type="orderStatus[row.status]?.type" size="small">{{ orderStatus[row.status]?.text }}</el-tag>
              </template>
            </el-table-column>
          </el-table>
        </div>

        <div class="page-card">
          <h3 class="page-title">近 7 日批次</h3>
          <el-table :data="batches" size="small">
            <el-table-column prop="batch_no" label="批次号" width="180" />
            <el-table-column prop="batch_date" label="日期" width="110" />
            <el-table-column label="餐别" width="80">
              <template #default="{ row }">{{ mealTypes[row.meal_type] }}</template>
            </el-table-column>
            <el-table-column prop="order_count" label="餐单数" width="80" />
            <el-table-column label="状态">
              <template #default="{ row }">
                <el-tag :type="row.status === 'released' ? 'success' : 'warning'" size="small">
                  {{ row.status === 'released' ? '已出餐' : '备餐中' }}
                </el-tag>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'
import { orderStatus, mealTypes } from '../utils'

const date = ref(new Date().toISOString().slice(0, 10))
const mealType = ref('lunch')
const summary = ref({ dishes: [], orders: [] })
const batches = ref([])
const batchDetail = ref({ items: [] })
const acting = ref(false)

async function load() {
  const [s, b] = await Promise.all([
    api.get('/kitchen/summary', { params: { date: date.value, meal_type: mealType.value } }),
    api.get('/kitchen/batches', { params: { date: date.value } })
  ])
  summary.value = s
  batches.value = b
  if (s.batch_id) {
    batchDetail.value = await api.get(`/kitchen/batches/${s.batch_id}`)
    batchDetail.value.items.forEach((it) => { it.actual_qty = it.planned_qty })
  } else {
    batchDetail.value = { items: [] }
  }
}

async function createBatch() {
  acting.value = true
  try {
    await api.post('/kitchen/batches', { date: date.value, meal_type: mealType.value })
    ElMessage.success('批次已创建，开始备餐')
    await load()
  } finally {
    acting.value = false
  }
}

async function releaseBatch() {
  const shortage = batchDetail.value.items.filter((it) => it.actual_qty < it.planned_qty)
  if (shortage.length > 0) {
    await ElMessageBox.confirm(
      `以下菜品实际出餐少于计划：${shortage.map((s) => `${s.dish_name}（少 ${s.planned_qty - s.actual_qty} 份）`).join('、')}，` +
      `将为受影响餐单生成「厨房少做」异常工单。确认出餐？`,
      '存在少做菜品',
      { type: 'warning', confirmButtonText: '确认出餐', cancelButtonText: '再检查一下' }
    )
  }
  acting.value = true
  try {
    const res = await api.post(`/kitchen/batches/${summary.value.batch_id}/release`, {
      items: batchDetail.value.items.map((it) => ({ dish_name: it.dish_name, actual_qty: it.actual_qty }))
    })
    ElMessage.success(res.shortage_orders > 0 ? `已出餐，${res.shortage_orders} 单因少做转入异常处理` : '出餐完成，配送任务已生成')
    await load()
  } finally {
    acting.value = false
  }
}

onMounted(load)
</script>
