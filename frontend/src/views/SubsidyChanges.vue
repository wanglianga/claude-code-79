<template>
  <div class="page-card">
    <h3 class="page-title">补贴资格变更记录</h3>
    <el-table :data="list" v-loading="loading">
      <el-table-column prop="id" label="#" width="60" />
      <el-table-column prop="elder_name" label="老人" width="100" />
      <el-table-column label="变更前" width="140">
        <template #default="{ row }">{{ subsidyLevels[row.old_level] }}（{{ row.old_amount }}元/餐）</template>
      </el-table-column>
      <el-table-column label="变更后" width="140">
        <template #default="{ row }">
          <b>{{ subsidyLevels[row.new_level] }}（{{ row.new_amount }}元/餐）</b>
        </template>
      </el-table-column>
      <el-table-column prop="reason" label="变更原因" min-width="200" />
      <el-table-column prop="affected_orders" label="重算餐单" width="90" />
      <el-table-column prop="changed_by" label="操作人" width="100" />
      <el-table-column prop="created_at" label="变更时间" width="170">
        <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
      </el-table-column>
    </el-table>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import api from '../api'
import { subsidyLevels, fmtTime } from '../utils'

const list = ref([])
const loading = ref(false)

onMounted(async () => {
  loading.value = true
  try {
    list.value = await api.get('/finance/subsidy-changes')
  } finally {
    loading.value = false
  }
})
</script>
