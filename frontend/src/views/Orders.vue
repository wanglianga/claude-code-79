<template>
  <div class="page-card">
    <h3 class="page-title">餐单管理</h3>
    <div class="flex-row mb-12">
      <el-date-picker v-model="filters.date" type="date" value-format="YYYY-MM-DD" placeholder="按日期筛选"
        clearable @change="load" />
      <el-select v-model="filters.status" placeholder="按状态筛选" clearable style="width: 190px" @change="load">
        <el-option v-for="(v, k) in orderStatus" :key="k" :label="v.text" :value="k" />
      </el-select>
      <el-button @click="load" :icon="Search">查询</el-button>
      <el-button v-if="['family','elder','community','admin'].includes(store.role)" type="primary"
        @click="$router.push('/order/new')">新建订餐</el-button>
    </div>
    <el-table :data="list" v-loading="loading" @row-click="(r) => $router.push(`/orders/${r.id}`)" row-style="cursor:pointer">
      <el-table-column prop="order_no" label="单号" width="150" />
      <el-table-column prop="elder_name" label="老人" width="100" />
      <el-table-column prop="meal_date" label="用餐日期" width="110" />
      <el-table-column label="餐别" width="70">
        <template #default="{ row }">{{ mealTypes[row.meal_type] }}</template>
      </el-table-column>
      <el-table-column label="取餐方式" width="120">
        <template #default="{ row }">{{ deliveryTypes[row.delivery_type] }}</template>
      </el-table-column>
      <el-table-column label="来源" width="100">
        <template #default="{ row }">{{ orderSources[row.order_source] }}</template>
      </el-table-column>
      <el-table-column label="金额/补贴" width="150">
        <template #default="{ row }">
          {{ fmtMoney(row.total_amount) }} / <span class="danger-text">{{ fmtMoney(row.subsidy_amount) }}</span>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="170">
        <template #default="{ row }">
          <el-tag :type="orderStatus[row.status]?.type">{{ orderStatus[row.status]?.text || row.status }}</el-tag>
          <el-tag v-if="row.strict_mode" type="danger" size="small" effect="dark" style="margin-left:4px">严格</el-tag>
          <el-tag v-if="row.is_holiday_special" type="warning" size="small" style="margin-left:4px">{{ row.holiday_name || '节日加餐' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="created_by" label="下单人" width="100" />
    </el-table>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { Search } from '@element-plus/icons-vue'
import api from '../api'
import { store } from '../store'
import { orderStatus, mealTypes, deliveryTypes, orderSources, fmtMoney } from '../utils'

const list = ref([])
const loading = ref(false)
const filters = reactive({ date: '', status: '' })

async function load() {
  loading.value = true
  try {
    const params = {}
    if (filters.date) params.date = filters.date
    if (filters.status) params.status = filters.status
    list.value = await api.get('/orders', { params })
  } finally {
    loading.value = false
  }
}
onMounted(load)
</script>
