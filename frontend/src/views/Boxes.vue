<template>
  <div class="page-card">
    <div class="flex-row" style="justify-content: space-between">
      <h3 class="page-title" style="margin:0">餐盒回收台账</h3>
      <div class="flex-row">
        <el-select v-model="status" placeholder="回收状态" clearable style="width: 140px" @change="load">
          <el-option label="待回收" value="pending" />
          <el-option label="部分回收" value="partial" />
          <el-option label="已回收" value="returned" />
        </el-select>
        <el-button @click="load" :icon="Search">查询</el-button>
      </div>
    </div>
    <el-table :data="list" v-loading="loading" class="mt-12">
      <el-table-column prop="order_no" label="餐单号" width="150">
        <template #default="{ row }">
          <el-link type="primary" @click="$router.push(`/orders/${row.order_id}`)">{{ row.order_no }}</el-link>
        </template>
      </el-table-column>
      <el-table-column prop="elder_name" label="老人" width="100" />
      <el-table-column prop="meal_date" label="用餐日期" width="110" />
      <el-table-column label="回收进度" width="120">
        <template #default="{ row }">{{ row.boxes_returned }} / {{ row.boxes_issued }} 个</template>
      </el-table-column>
      <el-table-column label="回收方式" width="130">
        <template #default="{ row }">{{ boxMethods[row.return_method] }}</template>
      </el-table-column>
      <el-table-column label="状态" width="110">
        <template #default="{ row }">
          <el-tag :type="boxStatus[row.status]?.type">{{ boxStatus[row.status]?.text }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="returned_at" label="最近回收时间" width="170">
        <template #default="{ row }">{{ fmtTime(row.returned_at) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="180" fixed="right">
        <template #default="{ row }">
          <template v-if="row.status !== 'returned'">
            <el-button v-if="canOperate" size="small" type="primary" @click="openReturn(row)">登记回收</el-button>
            <el-button v-if="canOperate" size="small" type="warning" plain @click="urge(row)">催回</el-button>
          </template>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="returnVisible" title="登记餐盒回收" width="420px">
      <el-form label-width="90px">
        <el-form-item label="回收数量">
          <el-input-number v-model="returnCount" :min="1" :max="(current?.boxes_issued || 1) - (current?.boxes_returned || 0)" />
          <span class="muted" style="margin-left:8px">
            剩余 {{ (current?.boxes_issued || 0) - (current?.boxes_returned || 0) }} 个
          </span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="returnVisible = false">取消</el-button>
        <el-button type="primary" :loading="acting" @click="doReturn">确认回收</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { Search } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'
import { store } from '../store'
import { boxMethods, boxStatus, fmtTime } from '../utils'

const list = ref([])
const loading = ref(false)
const acting = ref(false)
const status = ref('')
const current = ref(null)
const returnVisible = ref(false)
const returnCount = ref(1)

const canOperate = computed(() => ['community', 'admin', 'rider', 'volunteer'].includes(store.role))

async function load() {
  loading.value = true
  try {
    const params = {}
    if (status.value) params.status = status.value
    list.value = await api.get('/boxes', { params })
  } finally {
    loading.value = false
  }
}

function openReturn(row) {
  current.value = row
  returnCount.value = row.boxes_issued - row.boxes_returned
  returnVisible.value = true
}

async function doReturn() {
  acting.value = true
  try {
    await api.post(`/boxes/${current.value.id}/return`, { count: returnCount.value })
    ElMessage.success('回收登记成功')
    returnVisible.value = false
    await load()
  } finally {
    acting.value = false
  }
}

async function urge(row) {
  await ElMessageBox.confirm(`将为餐单 ${row.order_no} 生成「餐盒未回收」异常工单并通知社区，确认？`, '催回餐盒', {
    type: 'warning'
  })
  await api.post(`/boxes/${row.id}/urge`)
  ElMessage.success('已生成催回工单')
}

onMounted(load)
</script>
