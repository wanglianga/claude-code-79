<template>
  <div>
    <div class="page-card">
      <h3 class="page-title">志愿者资质与帮送考核</h3>
      <div class="muted" style="margin-bottom:12px">
        发车前核验：身份核验、所属社区/组织、助餐培训、当日健康、帮送资格。累计帮送次数、异常率、老人评价进入志愿记录；连续异常担责或未完成培训的自动暂停帮送资格。
      </div>
      <el-table :data="list" v-loading="loading">
        <el-table-column prop="name" label="志愿者" width="90" />
        <el-table-column prop="phone" label="电话" width="120" />
        <el-table-column prop="org" label="所属社区/组织" min-width="150" />
        <el-table-column label="身份" width="80">
          <template #default="{ row }">
            <el-tag :type="row.id_verified?'success':'danger'" size="small">{{ row.id_verified?'已核验':'未核验' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="助餐培训" width="150">
          <template #default="{ row }">
            <el-tag :type="row.trained?'success':'danger'" size="small">{{ row.trained?('已培训'+(row.training_date?' '+row.training_date.slice(0,10):'')):'未培训' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="当日健康" width="90">
          <template #default="{ row }">
            <el-tag v-if="row.today_health==='healthy'" type="success" size="small">健康</el-tag>
            <el-tag v-else-if="row.today_health==='unwell'" type="danger" size="small">不适</el-tag>
            <el-tag v-else type="info" size="small">未打卡</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="帮送资格" width="150">
          <template #default="{ row }">
            <el-tag :type="row.eligible?'success':'danger'" size="small">{{ row.eligible?'正常':'已暂停' }}</el-tag>
            <div v-if="!row.eligible" class="danger-text" style="font-size:12px">{{ row.suspend_reason }}</div>
          </template>
        </el-table-column>
        <el-table-column label="帮送/待核实" width="100">
          <template #default="{ row }">
            {{ row.delivered_count }} 单<span v-if="row.pending_verify" class="danger-text"> / 待核{{ row.pending_verify }}</span>
          </template>
        </el-table-column>
        <el-table-column label="异常率" width="90">
          <template #default="{ row }">{{ row.anomaly_rate.toFixed(0) }}%（{{ row.responsible_count }}担责）</template>
        </el-table-column>
        <el-table-column label="老人评价" width="85">
          <template #default="{ row }">{{ row.avg_rating ? row.avg_rating + ' 星' : '—' }}</template>
        </el-table-column>
        <el-table-column label="操作" width="100" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEdit(row)">资质/资格</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog v-model="dlg" title="维护志愿者资质与帮送资格" width="520px">
      <el-form label-width="120px" v-if="form.id">
        <el-form-item label="志愿者">{{ form.name }}</el-form-item>
        <el-form-item label="所属社区/组织">
          <el-input v-model="form.org" />
        </el-form-item>
        <el-form-item label="身份已核验">
          <el-switch v-model="form.id_verified" />
        </el-form-item>
        <el-form-item label="已完成培训">
          <el-switch v-model="form.trained" />
        </el-form-item>
        <el-form-item label="培训日期">
          <el-date-picker v-model="form.training_date" type="date" value-format="YYYY-MM-DD" />
        </el-form-item>
        <el-form-item label="帮送资格">
          <el-switch v-model="form.eligible" active-text="允许帮送" inactive-text="暂停资格" />
        </el-form-item>
        <el-form-item label="暂停原因">
          <el-input v-model="form.suspend_reason" type="textarea" :rows="2" placeholder="如：连续异常、未培训复训中" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dlg=false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../api'

const list = ref([])
const loading = ref(false)
const saving = ref(false)
const dlg = ref(false)
const form = reactive({})

async function load() {
  loading.value = true
  try { list.value = await api.get('/volunteers/manage') } finally { loading.value = false }
}
function openEdit(row) {
  Object.assign(form, {
    id: row.id, name: row.name, org: row.org, id_verified: row.id_verified,
    trained: row.trained, training_date: row.training_date ? row.training_date.slice(0, 10) : '',
    eligible: row.eligible, suspend_reason: row.suspend_reason
  })
  dlg.value = true
}
async function save() {
  saving.value = true
  try {
    await api.put(`/volunteers/${form.id}`, form)
    ElMessage.success('已保存')
    dlg.value = false
    await load()
  } finally { saving.value = false }
}
onMounted(load)
</script>
