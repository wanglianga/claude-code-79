<template>
  <div class="page-card">
    <div class="flex-row" style="justify-content: space-between">
      <h3 class="page-title" style="margin:0">异常工单（异常触发社区回访与补贴核销联动）</h3>
      <div class="flex-row">
        <el-select v-model="filters.status" placeholder="状态" clearable style="width: 140px" @change="load">
          <el-option label="待处理" value="open" />
          <el-option label="回访处理中" value="processing" />
          <el-option label="已办结" value="resolved" />
        </el-select>
        <el-select v-model="filters.type" placeholder="类型" clearable style="width: 160px" @change="load">
          <el-option v-for="(v, k) in anomalyTypes" :key="k" :label="v" :value="k" />
        </el-select>
        <el-button @click="load" :icon="Search">查询</el-button>
      </div>
    </div>

    <el-table :data="list" v-loading="loading" class="mt-12">
      <el-table-column prop="id" label="工单" width="70">
        <template #default="{ row }">#{{ row.id }}</template>
      </el-table-column>
      <el-table-column label="类型" width="150">
        <template #default="{ row }">
          <el-tag :type="row.priority === 'high' ? 'danger' : 'info'" effect="plain">{{ row.type_name }}</el-tag>
          <el-tag v-if="row.home_visit" type="danger" size="small" effect="dark" style="margin-left:2px">已发起上门</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="elder_name" label="老人" width="90" />
      <el-table-column label="关联餐单" width="140">
        <template #default="{ row }">
          <el-link v-if="row.order_id" type="primary" @click="$router.push(`/orders/${row.order_id}`)">
            {{ row.order_no }}
          </el-link>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column prop="description" label="描述" min-width="240" show-overflow-tooltip />
      <el-table-column label="责任/食品安全" width="140">
        <template #default="{ row }">
          <template v-if="row.type === 'volunteer_delivery'">
            <el-tag :type="(responsibleMap[row.responsible_party]||{}).type || 'info'" size="small">
              {{ row.responsible_party_name || '待划分' }}
            </el-tag>
            <el-tag v-if="row.food_safety" type="danger" size="small" effect="dark" style="margin-left:2px">食安</el-tag>
            <el-tag v-if="row.elder_unwell" type="danger" size="small" effect="plain" style="margin-left:2px">老人不适</el-tag>
          </template>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="110">
        <template #default="{ row }">
          <el-tag :type="anomalyStatus[row.status]?.type">{{ anomalyStatus[row.status]?.text }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="回访" width="70">
        <template #default="{ row }">{{ row.follow_up_count }} 次</template>
      </el-table-column>
      <el-table-column label="连续未开门" width="90">
        <template #default="{ row }">
          <span v-if="row.type === 'no_answer'" :class="{ 'danger-text': row.no_answer_count >= 2 }">
            {{ row.no_answer_count }} 次
          </span>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column prop="created_at" label="创建时间" width="160">
        <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="300" fixed="right">
        <template #default="{ row }">
          <template v-if="row.status !== 'resolved' && canHandle">
            <el-button v-if="row.type === 'volunteer_delivery'" size="small" type="primary" @click="openInvestigate(row)">社区核实/责任</el-button>
            <el-button size="small" type="warning" @click="openFollowUp(row)">回访</el-button>
            <el-button v-if="row.type === 'no_answer' && !row.home_visit" size="small" type="danger"
              :disabled="row.no_answer_count < 2"
              :title="row.no_answer_count < 2 ? '连续未开门未达阈值（≥2 次）' : '发起上门查看'"
              @click="homeVisit(row)">上门查看</el-button>
            <el-button size="small" type="success" @click="openResolve(row)">办结</el-button>
          </template>
          <el-tooltip v-if="row.resolution" :content="(row.investigation ? '核实：'+row.investigation+'；' : '') + row.resolution">
            <span class="muted" style="cursor:help">处理结果</span>
          </el-tooltip>
        </template>
      </el-table-column>
    </el-table>

    <!-- 回访 -->
    <el-dialog v-model="followUpVisible" :title="`社区回访（工单 #${current?.id}）`" width="480px">
      <el-form label-width="90px">
        <el-form-item label="回访方式" required>
          <el-radio-group v-model="followUpForm.type">
            <el-radio-button value="phone">电话回访</el-radio-button>
            <el-radio-button value="visit">上门回访</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="老人状态" required>
          <el-radio-group v-model="followUpForm.elder_status">
            <el-radio-button value="fine">安好</el-radio-button>
            <el-radio-button value="need_help">需要协助</el-radio-button>
            <el-radio-button value="urgent">紧急</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="回访结果" required>
          <el-input v-model="followUpForm.result" type="textarea" placeholder="记录回访了解到的情况" />
        </el-form-item>
        <el-form-item label="后续配送方式">
          <el-select v-model="followUpForm.delivery_confirm_mode" clearable placeholder="不调整" style="width:100%">
            <el-option label="直接上门（默认）" value="direct" />
            <el-option label="电话确认后再上门" value="phone_first" />
          </el-select>
        </el-form-item>
      </el-form>
      <el-alert v-if="followUpForm.elder_status === 'urgent'" type="error" :closable="false" show-icon
        title="紧急状态将立即通知社区负责人与家属" />
      <el-alert v-else-if="followUpForm.elder_status === 'need_help'" type="warning" :closable="false" show-icon
        title="将老人风险标签调整为「关注」并生成次日重点关注，骑手端提前可见" />
      <template #footer>
        <el-button @click="followUpVisible = false">取消</el-button>
        <el-button type="primary" :loading="acting" @click="doFollowUp">提交回访</el-button>
      </template>
    </el-dialog>

    <!-- 办结 -->
    <el-dialog v-model="resolveVisible" :title="`办结工单 #${current?.id}`" width="480px">
      <el-form label-width="90px">
        <el-form-item label="处理方式" required>
          <el-radio-group v-model="resolveForm.action">
            <el-radio-button value="close">关闭工单</el-radio-button>
            <el-radio-button value="redeliver" :disabled="!current?.order_id">重新配送</el-radio-button>
            <el-radio-button value="refund" :disabled="!current?.order_id">退餐退款</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="处理结果" required>
          <el-input v-model="resolveForm.resolution" type="textarea" placeholder="处理结论将同步到餐单时间线与核销档案" />
        </el-form-item>
      </el-form>
      <el-alert v-if="resolveForm.action === 'refund'" type="warning" :closable="false" show-icon
        title="退餐退款后该单不纳入本月补贴发放" />
      <el-alert v-else-if="resolveForm.action === 'redeliver'" type="info" :closable="false" show-icon
        title="餐单将回到待配送状态，重新进入配送任务池" />
      <template #footer>
        <el-button @click="resolveVisible = false">取消</el-button>
        <el-button type="success" :loading="acting" @click="doResolve">确认办结</el-button>
      </template>
    </el-dialog>
    <!-- 社区核实：志愿者帮送异常（路线/取餐/照片/老人反馈 → 责任划分 → 补送/退餐/重新签收） -->
    <el-dialog v-model="investigateVisible" :title="`社区核实帮送异常（工单 #${current?.id}）`" width="560px">
      <el-alert type="info" :closable="false" show-icon class="mb-12"
        title="请核实志愿者路线、取餐时间、送达照片与老人反馈；涉及食品安全或老人不适，志愿者、厨房、社区各自记录责任，不计骑手考核。" />
      <el-form label-width="110px">
        <el-form-item label="核实记录" required>
          <el-input v-model="investForm.investigation" type="textarea" :rows="3"
            placeholder="路线、取餐时间、送达照片、老人/邻里反馈等核实情况" />
        </el-form-item>
        <el-form-item label="处理结论" required>
          <el-radio-group v-model="investForm.outcome">
            <el-radio value="redeliver">安排补送</el-radio>
            <el-radio value="refund">退餐退款</el-radio>
            <el-radio value="resign">重新签收</el-radio>
            <el-radio value="none">无责关闭</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="责任划分" required>
          <el-radio-group v-model="investForm.responsible">
            <el-radio value="volunteer">志愿者</el-radio>
            <el-radio value="kitchen">厨房</el-radio>
            <el-radio value="community">社区</el-radio>
            <el-radio value="none">无责</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="情形标记">
          <el-checkbox v-model="investForm.food_safety">涉及食品安全</el-checkbox>
          <el-checkbox v-model="investForm.elder_unwell">老人用餐后不适</el-checkbox>
        </el-form-item>
        <el-form-item label="补充说明">
          <el-input v-model="investForm.resolution_note" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="investigateVisible=false">取消</el-button>
        <el-button type="primary" :loading="acting" @click="doInvestigate">提交核实结论</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { Search } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'
import { store } from '../store'
import { anomalyTypes, anomalyStatus, responsibleMap, fmtTime } from '../utils'

const list = ref([])
const loading = ref(false)
const acting = ref(false)
const filters = reactive({ status: '', type: '' })
const current = ref(null)
const followUpVisible = ref(false)
const resolveVisible = ref(false)
const investigateVisible = ref(false)
const followUpForm = reactive({ type: 'phone', elder_status: 'fine', result: '', delivery_confirm_mode: '' })
const resolveForm = reactive({ action: 'close', resolution: '' })
const investForm = reactive({ investigation: '', outcome: 'redeliver', responsible: 'volunteer', food_safety: false, elder_unwell: false, resolution_note: '' })

const canHandle = computed(() => ['community', 'admin'].includes(store.role))

async function load() {
  loading.value = true
  try {
    const params = {}
    if (filters.status) params.status = filters.status
    if (filters.type) params.type = filters.type
    list.value = await api.get('/anomalies', { params })
  } finally {
    loading.value = false
  }
}

function openFollowUp(row) {
  current.value = row
  followUpForm.type = 'phone'
  followUpForm.elder_status = 'fine'
  followUpForm.result = ''
  followUpForm.delivery_confirm_mode = ''
  followUpVisible.value = true
}

async function homeVisit(row) {
  await ElMessageBox.confirm(
    `老人「${row.elder_name}」已连续 ${row.no_answer_count} 次未开门，确认发起上门查看？将通知家属与社区网格员。`,
    '发起上门查看',
    { type: 'warning', confirmButtonText: '确认发起', cancelButtonText: '取消' }
  )
  await api.post(`/anomalies/${row.id}/home-visit`)
  ElMessage.success('已发起上门查看，请尽快上门并登记回访结果')
  await load()
}

async function doFollowUp() {
  if (!followUpForm.result) {
    ElMessage.warning('请填写回访结果')
    return
  }
  acting.value = true
  try {
    await api.post(`/anomalies/${current.value.id}/followups`, followUpForm)
    ElMessage.success('回访已记录')
    followUpVisible.value = false
    await load()
  } finally {
    acting.value = false
  }
}

function openResolve(row) {
  current.value = row
  resolveForm.action = 'close'
  resolveForm.resolution = ''
  resolveVisible.value = true
}

async function doResolve() {
  if (!resolveForm.resolution) {
    ElMessage.warning('请填写处理结果')
    return
  }
  acting.value = true
  try {
    await api.post(`/anomalies/${current.value.id}/resolve`, resolveForm)
    ElMessage.success('工单已办结')
    resolveVisible.value = false
    await load()
  } finally {
    acting.value = false
  }
}

function openInvestigate(row) {
  current.value = row
  Object.assign(investForm, { investigation: '', outcome: 'redeliver', responsible: 'volunteer', food_safety: !!row.food_safety, elder_unwell: !!row.elder_unwell, resolution_note: '' })
  investigateVisible.value = true
}
async function doInvestigate() {
  if (!investForm.investigation) {
    ElMessage.warning('请填写核实记录')
    return
  }
  acting.value = true
  try {
    const r = await api.post(`/anomalies/${current.value.id}/investigate`, investForm)
    ElMessage.success(`已核实：${ { redeliver: '安排补送', refund: '退餐退款', resign: '重新签收', none: '无责关闭' }[r.outcome] }，责任：${r.responsible}`)
    investigateVisible.value = false
    await load()
  } finally { acting.value = false }
}

onMounted(load)
</script>
