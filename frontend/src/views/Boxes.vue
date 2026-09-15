<template>
  <div>
    <div class="page-card">
      <div class="flex-row" style="justify-content: space-between">
        <h3 class="page-title" style="margin:0">餐盒回收与押金管理</h3>
        <div class="flex-row" v-if="inventory">
          <el-tag type="primary" size="large" effect="plain">餐盒库存：{{ inventory.stock }} 个</el-tag>
          <span class="muted">更新于 {{ fmtTime(inventory.updated_at) }}</span>
        </div>
      </div>
    </div>

    <el-tabs v-model="tab" class="page-card">
      <!-- 老人汇总与处置 -->
      <el-tab-pane label="老人汇总与处置" name="status">
        <el-table :data="elderStatus" v-loading="loadingStatus">
          <el-table-column prop="elder_name" label="老人" width="90" />
          <el-table-column label="类型" width="100">
            <template #default="{ row }">
              <el-tag :type="elderTypes[row.elder_type]?.type" size="small">{{ elderTypes[row.elder_type]?.text }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="未回收" width="80">
            <template #default="{ row }">
              <b :class="{ 'danger-text': row.over_threshold }">{{ row.unreturned }}</b> 个
            </template>
          </el-table-column>
          <el-table-column prop="remind_count" label="提醒次数" width="80" />
          <el-table-column prop="family_feedback" label="家属反馈" min-width="150" show-overflow-tooltip>
            <template #default="{ row }">{{ row.family_feedback || '—' }}</template>
          </el-table-column>
          <el-table-column label="押金状态" width="110">
            <template #default="{ row }">
              <el-tag :type="depositStatus[row.deposit_status]?.type" size="small">
                {{ depositStatus[row.deposit_status]?.text }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="餐盒策略" width="110">
            <template #default="{ row }">
              <el-tag :type="boxPolicies[row.box_policy]?.type" size="small" effect="plain">
                {{ boxPolicies[row.box_policy]?.text }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="操作" min-width="330" fixed="right">
            <template #default="{ row }">
              <template v-if="canOperate">
                <el-button v-if="row.unreturned > 0" size="small" type="primary" plain
                  @click="openDoorCollect(row)">上门回收</el-button>
                <el-button v-if="row.deposit_status === 'none' || row.deposit_status === 'refunded'" size="small"
                  type="warning" :disabled="!row.over_threshold"
                  :title="row.over_threshold ? '按老人类型收取押金' : '未归还未达阈值（≥3 个）'"
                  @click="charge(row)">收取押金</el-button>
                <el-button size="small" @click="openPolicy(row)">餐盒策略</el-button>
              </template>
              <el-button v-if="isFamilyOf(row)" size="small" type="primary" @click="openFeedback(row)">反馈</el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <!-- 押金台账 -->
      <el-tab-pane label="押金台账" name="deposits">
        <el-table :data="deposits" v-loading="loadingDeposits">
          <el-table-column prop="id" label="单号" width="70">
            <template #default="{ row }">#{{ row.id }}</template>
          </el-table-column>
          <el-table-column prop="elder_name" label="老人" width="90" />
          <el-table-column label="类型" width="90">
            <template #default="{ row }">
              <el-tag :type="elderTypes[row.elder_type]?.type" size="small">{{ elderTypes[row.elder_type]?.text }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="押金金额" width="100">
            <template #default="{ row }">{{ fmtMoney(row.amount) }}</template>
          </el-table-column>
          <el-table-column prop="unreturned_snapshot" label="未回收快照" width="90" />
          <el-table-column label="状态" width="110">
            <template #default="{ row }">
              <el-tag :type="depositStatus[row.status]?.type" size="small">{{ depositStatus[row.status]?.text }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="免押责任链" min-width="220">
            <template #default="{ row }">
              <div v-if="row.waive_applicant" class="muted">
                社区负责人：{{ row.waive_applicant }}<br v-if="row.waive_approver" />
                <span v-if="row.waive_approver">审批人：{{ row.waive_approver }}<br /></span>
                <span v-if="row.volunteer_name">回收志愿者：{{ row.volunteer_name }}</span>
              </div>
              <span v-else class="muted">—</span>
            </template>
          </el-table-column>
          <el-table-column prop="created_by" label="发起人" width="90" />
          <el-table-column label="操作" min-width="200" fixed="right">
            <template #default="{ row }">
              <el-button v-if="row.status === 'pending' && canPay(row)" size="small" type="primary"
                @click="pay(row)">缴纳押金</el-button>
              <el-button v-if="row.status === 'pending' && row.elder_type === 'difficult' && canOperate"
                size="small" type="success" @click="openWaive(row)">申请免押</el-button>
              <el-button v-if="row.status === 'waive_pending' && store.role === 'admin'" size="small" type="warning"
                @click="approve(row)">审批免押</el-button>
              <el-button v-if="row.status === 'paid' && canOperate" size="small" type="info"
                @click="refund(row)">退还押金</el-button>
              <el-button v-if="row.status === 'waived' && store.role === 'volunteer' && row.volunteer_id === store.user?.id"
                size="small" type="primary" @click="openVolunteerCollect(row)">志愿回收登记</el-button>
            </template>
          </el-table-column>
        </el-table>
        <div v-if="deposits.length === 0" class="muted mt-12">暂无押金单</div>
      </el-tab-pane>

      <!-- 回收台账 -->
      <el-tab-pane label="回收台账" name="records">
        <div class="flex-row mb-12">
          <el-select v-model="status" placeholder="回收状态" clearable style="width: 140px" @change="loadRecords">
            <el-option label="待回收" value="pending" />
            <el-option label="部分回收" value="partial" />
            <el-option label="已回收" value="returned" />
          </el-select>
          <el-button @click="loadRecords" :icon="Search">查询</el-button>
        </div>
        <el-table :data="list" v-loading="loading">
          <el-table-column prop="order_no" label="餐单号" width="150">
            <template #default="{ row }">
              <el-link type="primary" @click="$router.push(`/orders/${row.order_id}`)">{{ row.order_no }}</el-link>
            </template>
          </el-table-column>
          <el-table-column prop="elder_name" label="老人" width="90" />
          <el-table-column prop="meal_date" label="用餐日期" width="110" />
          <el-table-column label="回收进度" width="110">
            <template #default="{ row }">{{ row.boxes_returned }} / {{ row.boxes_issued }} 个</template>
          </el-table-column>
          <el-table-column label="回收方式" width="120">
            <template #default="{ row }">{{ boxMethods[row.return_method] }}</template>
          </el-table-column>
          <el-table-column label="状态" width="100">
            <template #default="{ row }">
              <el-tag :type="boxStatus[row.status]?.type">{{ boxStatus[row.status]?.text }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="160" fixed="right">
            <template #default="{ row }">
              <template v-if="row.status !== 'returned'">
                <el-button v-if="canOperate" size="small" type="primary" @click="openReturn(row)">登记回收</el-button>
                <el-button v-if="canOperate" size="small" type="warning" plain @click="urge(row)">催回</el-button>
              </template>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>
    </el-tabs>

    <!-- 登记回收 -->
    <el-dialog v-model="returnVisible" title="登记餐盒回收" width="420px">
      <el-form label-width="90px">
        <el-form-item label="回收数量">
          <el-input-number v-model="returnCount" :min="1" :max="(current?.boxes_issued || 1) - (current?.boxes_returned || 0)" />
          <span class="muted" style="margin-left:8px">剩余 {{ (current?.boxes_issued || 0) - (current?.boxes_returned || 0) }} 个</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="returnVisible = false">取消</el-button>
        <el-button type="primary" :loading="acting" @click="doReturn">确认回收</el-button>
      </template>
    </el-dialog>

    <!-- 上门回收 -->
    <el-dialog v-model="doorVisible" title="上门回收餐盒" width="420px">
      <p>老人「{{ current?.elder_name }}」未归还 {{ current?.unreturned }} 个餐盒</p>
      <el-form label-width="90px">
        <el-form-item label="回收数量">
          <el-input-number v-model="doorCount" :min="1" :max="current?.unreturned || 1" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="doorVisible = false">取消</el-button>
        <el-button type="primary" :loading="acting" @click="doDoorCollect">确认回收</el-button>
      </template>
    </el-dialog>

    <!-- 餐盒策略 -->
    <el-dialog v-model="policyVisible" title="调整餐盒策略" width="440px">
      <el-form label-width="90px">
        <el-form-item label="策略">
          <el-radio-group v-model="policyForm.policy">
            <el-radio-button value="normal">正常发放</el-radio-button>
            <el-radio-button value="disposable">改用一次性餐盒</el-radio-button>
            <el-radio-button value="paused">暂停新增发放</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="原因">
          <el-input v-model="policyForm.reason" type="textarea" placeholder="如：老人连续未归还，暂停发放可循环餐盒" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="policyVisible = false">取消</el-button>
        <el-button type="primary" :loading="acting" @click="doPolicy">确认调整</el-button>
      </template>
    </el-dialog>

    <!-- 免押申请 -->
    <el-dialog v-model="waiveVisible" title="困难老人免押申请" width="440px">
      <el-alert type="info" :closable="false" show-icon class="mb-12"
        title="免押审批将留存社区负责人（申请人）、审批人与回收志愿者，防止餐盒责任空转" />
      <el-form label-width="90px">
        <el-form-item label="回收志愿者" required>
          <el-select v-model="waiveForm.volunteer_id" style="width:100%">
            <el-option v-for="v in volunteers" :key="v.id" :label="v.name" :value="v.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="免押原因" required>
          <el-input v-model="waiveForm.reason" type="textarea" placeholder="如：老人低保困难，行动不便，由志愿者上门回收" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="waiveVisible = false">取消</el-button>
        <el-button type="success" :loading="acting" @click="doWaiveApply">提交申请</el-button>
      </template>
    </el-dialog>

    <!-- 志愿回收登记 -->
    <el-dialog v-model="volCollectVisible" title="志愿回收登记" width="420px">
      <p>为老人「{{ current?.elder_name }}」上门回收餐盒，回收结果将更新餐盒库存</p>
      <el-form label-width="90px">
        <el-form-item label="回收数量">
          <el-input-number v-model="volCount" :min="1" :max="99" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="volNote" placeholder="如：老人家属代交" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="volCollectVisible = false">取消</el-button>
        <el-button type="primary" :loading="acting" @click="doVolunteerCollect">确认回收</el-button>
      </template>
    </el-dialog>

    <!-- 家属反馈 -->
    <el-dialog v-model="feedbackVisible" title="餐盒反馈" width="420px">
      <el-input v-model="feedbackContent" type="textarea" placeholder="如：老人行动不便，下周集中归还" />
      <template #footer>
        <el-button @click="feedbackVisible = false">取消</el-button>
        <el-button type="primary" :loading="acting" @click="doFeedback">提交反馈</el-button>
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
import { boxMethods, boxStatus, boxPolicies, depositStatus, elderTypes, fmtTime, fmtMoney } from '../utils'

const tab = ref('status')
const list = ref([])
const elderStatus = ref([])
const deposits = ref([])
const inventory = ref(null)
const volunteers = ref([])
const loading = ref(false)
const loadingStatus = ref(false)
const loadingDeposits = ref(false)
const acting = ref(false)
const status = ref('')
const current = ref(null)

const returnVisible = ref(false)
const doorVisible = ref(false)
const policyVisible = ref(false)
const waiveVisible = ref(false)
const volCollectVisible = ref(false)
const feedbackVisible = ref(false)
const returnCount = ref(1)
const doorCount = ref(1)
const volCount = ref(1)
const volNote = ref('')
const feedbackContent = ref('')
const policyForm = reactive({ policy: 'normal', reason: '' })
const waiveForm = reactive({ volunteer_id: null, reason: '' })

const canOperate = computed(() => ['community', 'admin'].includes(store.role))

function isFamilyOf() {
  return ['family', 'elder'].includes(store.role)
}
function canPay(row) {
  return ['family', 'elder', 'community', 'admin'].includes(store.role) && row.status === 'pending'
}

async function loadRecords() {
  loading.value = true
  try {
    const params = {}
    if (status.value) params.status = status.value
    list.value = await api.get('/boxes', { params })
  } finally {
    loading.value = false
  }
}
async function loadStatus() {
  loadingStatus.value = true
  try {
    elderStatus.value = await api.get('/boxes/elder-status')
  } finally {
    loadingStatus.value = false
  }
}
async function loadDeposits() {
  loadingDeposits.value = true
  try {
    deposits.value = await api.get('/deposits')
  } finally {
    loadingDeposits.value = false
  }
}
async function loadInventory() {
  try {
    inventory.value = await api.get('/boxes/inventory')
  } catch (e) { /* 忽略 */ }
}
async function loadAll() {
  await Promise.all([loadRecords(), loadStatus(), loadDeposits(), loadInventory()])
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
    await loadAll()
  } finally {
    acting.value = false
  }
}
async function urge(row) {
  await ElMessageBox.confirm(`将为餐单 ${row.order_no} 生成「餐盒未回收」异常工单并记录提醒次数，确认？`, '催回餐盒', { type: 'warning' })
  await api.post(`/boxes/${row.id}/urge`)
  ElMessage.success('已生成催回工单')
  await loadAll()
}

function openDoorCollect(row) {
  current.value = row
  doorCount.value = row.unreturned
  doorVisible.value = true
}
async function doDoorCollect() {
  acting.value = true
  try {
    const res = await api.post('/boxes/door-collect', { elder_id: current.value.elder_id, count: doorCount.value })
    ElMessage.success(`上门回收 ${res.collected} 个餐盒，库存已更新`)
    doorVisible.value = false
    await loadAll()
  } finally {
    acting.value = false
  }
}

function openPolicy(row) {
  current.value = row
  policyForm.policy = row.box_policy
  policyForm.reason = ''
  policyVisible.value = true
}
async function doPolicy() {
  acting.value = true
  try {
    await api.post(`/elders/${current.value.elder_id}/box-policy`, policyForm)
    ElMessage.success('餐盒策略已调整')
    policyVisible.value = false
    await loadAll()
  } finally {
    acting.value = false
  }
}

async function charge(row) {
  const amount = row.elder_type === 'difficult' ? 10 : 20
  await ElMessageBox.confirm(
    `老人「${row.elder_name}」未归还 ${row.unreturned} 个餐盒（≥3 阈值），按${elderTypes[row.elder_type]?.text}规则收取押金 ${amount} 元，确认？`,
    '收取餐盒押金',
    { type: 'warning' }
  )
  await api.post(`/elders/${row.elder_id}/deposit`)
  ElMessage.success('押金单已创建并通知家属')
  await loadAll()
}

async function pay(row) {
  await ElMessageBox.confirm(`确认缴纳餐盒押金 ${fmtMoney(row.amount)}？归还餐盒后可申请退还`, '缴纳押金', { type: 'info' })
  await api.post(`/deposits/${row.id}/pay`)
  ElMessage.success('押金已缴纳')
  await loadAll()
}

function openWaive(row) {
  current.value = row
  waiveForm.volunteer_id = volunteers.value[0]?.id || null
  waiveForm.reason = ''
  waiveVisible.value = true
}
async function doWaiveApply() {
  if (!waiveForm.volunteer_id || !waiveForm.reason) {
    ElMessage.warning('请选择回收志愿者并填写免押原因')
    return
  }
  acting.value = true
  try {
    await api.post(`/deposits/${current.value.id}/waive-apply`, waiveForm)
    ElMessage.success('免押申请已提交，待平台管理员审批')
    waiveVisible.value = false
    await loadAll()
  } finally {
    acting.value = false
  }
}

async function approve(row) {
  await ElMessageBox.confirm(
    `批准老人「${row.elder_name}」免押？回收志愿者：${row.volunteer_name}。审批后志愿者可上门回收`,
    '免押审批',
    { type: 'warning' }
  )
  await api.post(`/deposits/${row.id}/waive-approve`)
  ElMessage.success('已批准免押，已通知志愿者回收')
  await loadAll()
}

async function refund(row) {
  await ElMessageBox.confirm(`确认退还老人「${row.elder_name}」押金 ${fmtMoney(row.amount)}？`, '退还押金', { type: 'info' })
  await api.post(`/deposits/${row.id}/refund`)
  ElMessage.success('押金已退还')
  await loadAll()
}

function openVolunteerCollect(row) {
  current.value = row
  volCount.value = 1
  volNote.value = ''
  volCollectVisible.value = true
}
async function doVolunteerCollect() {
  acting.value = true
  try {
    const res = await api.post('/boxes/volunteer-collect', {
      elder_id: current.value.elder_id, count: volCount.value, note: volNote.value
    })
    ElMessage.success(`志愿回收 ${res.collected} 个餐盒，库存已更新`)
    volCollectVisible.value = false
    await loadAll()
  } finally {
    acting.value = false
  }
}

function openFeedback(row) {
  current.value = row
  feedbackContent.value = ''
  feedbackVisible.value = true
}
async function doFeedback() {
  if (!feedbackContent.value) {
    ElMessage.warning('请填写反馈内容')
    return
  }
  acting.value = true
  try {
    // 家属反馈记录在该老人最近的未回收餐盒记录上
    const rec = list.value.find((r) => r.elder_name === current.value.elder_name && r.status !== 'returned') || list.value[0]
    await api.post(`/boxes/${rec.id}/family-feedback`, { content: feedbackContent.value })
    ElMessage.success('反馈已提交')
    feedbackVisible.value = false
    await loadAll()
  } finally {
    acting.value = false
  }
}

onMounted(async () => {
  await loadAll()
  if (canOperate.value) {
    volunteers.value = await api.get('/volunteers').catch(() => [])
  }
})
</script>
