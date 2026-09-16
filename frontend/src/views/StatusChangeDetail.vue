<template>
  <div v-loading="loading">
    <template v-if="d">
      <el-page-header @back="$router.push('/status-changes')" :content="`清算单 #${d.id}`" class="mb-12" />

      <div class="page-card">
        <div class="flex-row" style="justify-content:space-between">
          <h3 class="page-title" style="margin:0">
            {{ d.elder_name }} · {{ d.change_type_name }}
            <el-tag :type="changeTypeMap[d.change_type]?.type" size="small" style="margin-left:8px">{{ changeStatusMap[d.status]?.text }}</el-tag>
          </h3>
          <div class="flex-row">
            <el-button v-if="canResume" type="success" @click="resumeDlg=true">出院恢复 / 重新核验</el-button>
          </div>
        </div>
        <el-descriptions :column="3" border class="mt-12" size="small">
          <el-descriptions-item label="生效日期">{{ d.effective_date }}</el-descriptions-item>
          <el-descriptions-item label="经办人">{{ d.operator }}</el-descriptions-item>
          <el-descriptions-item label="家属确认人">{{ d.family_confirmed_by }}</el-descriptions-item>
          <el-descriptions-item v-if="d.hospital" label="住院/转入医院">{{ d.hospital }}</el-descriptions-item>
          <el-descriptions-item v-if="d.expected_discharge_date" label="预计出院日">{{ d.expected_discharge_date }}</el-descriptions-item>
          <el-descriptions-item v-if="d.family_contact_name || d.family_contact_phone" label="家属联系人">
            {{ d.family_contact_name }} {{ d.family_contact_phone }}
          </el-descriptions-item>
          <el-descriptions-item v-if="d.change_type==='hospitalization'||d.change_type==='transfer'" label="补贴资格保留">
            <el-tag :type="d.subsidy_retained?'success':'danger'" size="small">{{ d.subsidy_retained ? '保留' : '不保留（出院重核）' }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item v-if="d.change_type==='move_out'" label="新地址" :span="2">{{ d.new_address }}</el-descriptions-item>
          <el-descriptions-item v-if="d.change_type==='move_out'" label="覆盖判定">
            <el-tag :type="d.in_coverage?'success':'danger'" size="small">{{ d.in_coverage ? '仍在覆盖区' : '超出范围·停止配送' }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item v-if="d.death_date" label="去世日期">{{ d.death_date }}</el-descriptions-item>
          <el-descriptions-item label="登记时间">{{ fmtTime(d.created_at) }}</el-descriptions-item>
          <el-descriptions-item v-if="d.resumed_at" label="恢复时间">{{ fmtTime(d.resumed_at) }}</el-descriptions-item>
          <el-descriptions-item v-if="d.note" label="备注" :span="3">{{ d.note }}</el-descriptions-item>
        </el-descriptions>
      </div>

      <!-- 四段清算汇总 -->
      <el-row :gutter="14">
        <el-col :span="6">
          <div class="seg-card s1">
            <div class="seg-h">① 已签收 <span class="seg-count">{{ d.segments.signed }}</span></div>
            <div class="muted">保留签收记录，补贴正常财政结算</div>
            <div class="seg-money">{{ fmtMoney(d.signed_subsidy_total) }}</div>
          </div>
        </el-col>
        <el-col :span="6">
          <div class="seg-card s2">
            <div class="seg-h">② 在途（骑手已取餐） <span class="seg-count">{{ d.segments.in_transit }}</span></div>
            <div class="muted">不可按未服务核销；停止配送携回，补贴冻结</div>
            <div class="seg-money warn-text">冻结 {{ fmtMoney(d.transit_subsidy_total) }}</div>
          </div>
        </el-col>
        <el-col :span="6">
          <div class="seg-card s2">
            <div class="seg-h">③ 已备餐未出餐 <span class="seg-count">{{ d.segments.prepared }}</span></div>
            <div class="muted">记批次/数量/成本，可转配；搬离去世按退餐</div>
            <div class="seg-money warn-text">成本 {{ fmtMoney(d.prepared_cost_total) }}｜退 {{ fmtMoney(d.prepared_refund_total) }}</div>
          </div>
        </el-col>
        <el-col :span="6">
          <div class="seg-card s4">
            <div class="seg-h">④ 未备餐 <span class="seg-count">{{ d.segments.unprepared }}</span></div>
            <div class="muted">取消并停止后续配送，释放补贴额度</div>
            <div class="seg-money muted">释放 {{ fmtMoney(d.unprepared_cancel_subsidy) }}</div>
          </div>
        </el-col>
      </el-row>

      <!-- 清算结论 -->
      <div class="page-card">
        <h3 class="page-title">清算结论与责任</h3>
        <el-descriptions :column="3" border size="small">
          <el-descriptions-item label="已签收补贴（正常结算）">{{ fmtMoney(d.signed_subsidy_total) }}</el-descriptions-item>
          <el-descriptions-item label="在途冻结补贴（暂不核销）">{{ fmtMoney(d.transit_subsidy_total) }}</el-descriptions-item>
          <el-descriptions-item label="已备餐退餐金额">{{ fmtMoney(d.prepared_refund_total) }}</el-descriptions-item>
          <el-descriptions-item label="已备餐食材成本">{{ fmtMoney(d.prepared_cost_total) }}</el-descriptions-item>
          <el-descriptions-item label="食材损耗（厨房责任）">
            <b :class="Number(d.kitchen_loss_total)>0?'danger-text':''">{{ fmtMoney(d.kitchen_loss_total) }}</b>
          </el-descriptions-item>
          <el-descriptions-item label="已转配其他老人">{{ d.transferred_qty }} 份</el-descriptions-item>
          <el-descriptions-item label="餐盒/保温箱回收">
            {{ d.boxes_to_recover>0 ? `${d.boxes_recovered}/${d.boxes_to_recover}` : '无需回收' }}
            <el-button v-if="canRecover && d.boxes_recovered<d.boxes_to_recover" link type="primary" size="small"
              @click="recoverDlg=true">登记回收</el-button>
          </el-descriptions-item>
          <el-descriptions-item v-if="d.remaining_quota||d.monthly_quota" label="恢复后当月剩余可享次数">
            {{ d.monthly_quota>0 ? `${d.remaining_quota}/${d.monthly_quota} 次` : '不限次' }}
          </el-descriptions-item>
        </el-descriptions>
        <div class="muted mt-12">
          结算口径：未出餐部分不进财政结算；骑手已取餐/已出餐未送达不按未服务核销，记食材损耗与厨房责任，可转配给其他老人。
        </div>
      </div>

      <!-- 出院恢复重新核验 -->
      <div v-if="d.reverify && d.reverify.address" class="page-card">
        <h3 class="page-title">出院恢复重新核验（四要素）</h3>
        <el-descriptions :column="2" border size="small">
          <el-descriptions-item label="送餐地址">{{ d.reverify.address }}</el-descriptions-item>
          <el-descriptions-item label="饮食禁忌">{{ d.reverify.dietary || '无' }}</el-descriptions-item>
          <el-descriptions-item label="补贴资格">
            {{ subsidyLevels[d.reverify.subsidy_level] }} {{ d.reverify.subsidy_amount }} 元/餐
          </el-descriptions-item>
          <el-descriptions-item label="紧急联系人">{{ d.reverify.emergency_name }} {{ d.reverify.emergency_phone }}</el-descriptions-item>
        </el-descriptions>
      </div>

      <!-- 四段明细表 -->
      <div class="page-card">
        <div class="flex-row" style="justify-content:space-between">
          <h3 class="page-title" style="margin:0">四段清算明细</h3>
          <el-button v-if="canTransfer && transferable.length>0" type="warning" size="small" @click="transferDlg=true">
            已备餐/在途餐食转配（{{ transferable.length }}）
          </el-button>
        </div>
        <el-table :data="d.items" size="small" class="mt-12">
          <el-table-column label="段" width="170">
            <template #default="{ row }">
              <el-tag :type="segmentMap[row.segment]?.type" size="small">{{ segmentMap[row.segment]?.text }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="order_no" label="单号" width="150" />
          <el-table-column prop="meal_date" label="用餐日期" width="100" />
          <el-table-column label="批次/份数" width="140">
            <template #default="{ row }">{{ row.batch_no || '—' }} · {{ row.qty }}份<span v-if="row.picked"> · 已取餐</span></template>
          </el-table-column>
          <el-table-column label="金额（总额/补贴/自付/成本）" width="260">
            <template #default="{ row }">
              {{ fmtMoney(row.total_amount) }} / {{ fmtMoney(row.subsidy_amount) }} /
              {{ fmtMoney(row.payable_amount) }} / <span class="warn-text">{{ fmtMoney(row.material_cost) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="处理方式" min-width="280">
            <template #default="{ row }">
              <el-tag v-if="row.kitchen_responsible" type="danger" size="small" effect="plain">厨房责任</el-tag>
              <el-tag v-if="row.transferred" type="success" size="small" effect="plain">已转配</el-tag>
              <span style="margin-left:4px">{{ row.handling }}</span>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </template>

    <!-- 出院恢复对话框 -->
    <el-dialog v-model="resumeDlg" title="出院恢复：重新核验四要素" width="640px">
      <el-alert type="warning" :closable="false" show-icon class="mb-12"
        title="恢复时将重新核验送餐地址、饮食禁忌、补贴资格、紧急联系人；暂停期间挂起的在途/已备餐餐单一律按原异常餐单结清（退餐、补贴不核销），并重算当月剩余可享次数，避免重复核销。" />
      <el-form label-width="110px">
        <el-form-item label="恢复生效日期" required>
          <el-date-picker v-model="resume.effective_date" type="date" value-format="YYYY-MM-DD" />
        </el-form-item>
        <el-form-item label="送餐地址" required>
          <el-input v-model="resume.address" />
        </el-form-item>
        <el-form-item label="饮食禁忌">
          <el-input v-model="resume.dietary" placeholder="如 低盐;软烂;忌海鲜" />
        </el-form-item>
        <el-form-item label="补贴资格">
          <el-select v-model="resume.subsidy_level" style="width:150px">
            <el-option v-for="(v,k) in subsidyLevels" :key="k" :label="v" :value="k" />
          </el-select>
          <el-input-number v-model="resume.subsidy_amount" :min="0" :precision="2" style="margin-left:10px" /> 元/餐
        </el-form-item>
        <el-form-item label="紧急联系人">
          <el-input v-model="resume.emergency_contact_name" placeholder="姓名" style="width:150px" />
          <el-input v-model="resume.emergency_contact_phone" placeholder="电话" style="width:180px;margin-left:8px" />
        </el-form-item>
        <el-form-item label="家属确认人" required>
          <el-input v-model="resume.family_confirmed_by" style="width:240px" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="resumeDlg=false">取消</el-button>
        <el-button type="success" :loading="saving" @click="doResume">确认恢复并结清</el-button>
      </template>
    </el-dialog>

    <!-- 转配对话框 -->
    <el-dialog v-model="transferDlg" title="已备餐/在途餐食转配给其他老人" width="640px">
      <el-alert type="info" :closable="false" class="mb-12"
        title="厨房已按批次出餐、食材已消耗或骑手已取餐的餐食不能按未服务核销，可转配给其他在服老人，减少食材损耗。" />
      <el-checkbox-group v-model="transferItemIds">
        <el-checkbox v-for="it in transferable" :key="it.id" :value="it.id" style="display:block">
          {{ it.order_no }}（{{ it.qty }}份，成本 {{ fmtMoney(it.material_cost) }}，批次 {{ it.batch_no || '—' }}）
        </el-checkbox>
      </el-checkbox-group>
      <el-select v-model="transferTo" filterable placeholder="选择接收老人" class="mt-12" style="width:100%">
        <el-option v-for="e in activeElders" :key="e.id" :label="`${e.name}（${e.address}）`" :value="e.id" />
      </el-select>
      <template #footer>
        <el-button @click="transferDlg=false">取消</el-button>
        <el-button type="warning" :loading="saving" @click="doTransfer">确认转配</el-button>
      </template>
    </el-dialog>

    <!-- 回收对话框 -->
    <el-dialog v-model="recoverDlg" title="登记餐盒/保温箱回收" width="420px">
      <el-form label-width="100px">
        <el-form-item label="本次回收数">
          <el-input-number v-model="recoverCount" :min="1" :max="d.boxes_to_recover-d.boxes_recovered" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="recoverDlg=false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="doRecover">确认回收</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import api from '../api'
import { store } from '../store'
import { changeTypeMap, changeStatusMap, segmentMap, subsidyLevels, fmtMoney, fmtTime } from '../utils'

const route = useRoute()
const d = ref(null)
const loading = ref(false)
const saving = ref(false)
const canResume = computed(() => ['community', 'admin'].includes(store.role))
const canTransfer = computed(() => ['kitchen', 'community', 'admin'].includes(store.role))
const canRecover = computed(() => ['community', 'rider', 'admin'].includes(store.role))

const resumeDlg = ref(false)
const transferDlg = ref(false)
const recoverDlg = ref(false)
const resume = ref({})
const transferItemIds = ref([])
const transferTo = ref(null)
const activeElders = ref([])
const recoverCount = ref(1)

const transferable = computed(() =>
  d.value ? d.value.items.filter((i) => !i.transferred && ['in_transit', 'prepared_undelivered'].includes(i.segment)) : []
)

async function load() {
  loading.value = true
  try {
    d.value = await api.get(`/status-changes/${route.params.id}`)
    resume.value = {
      effective_date: d.value.effective_date,
      address: d.value.elder_current.address,
      dietary: d.value.elder_current.dietary,
      subsidy_level: 'partial',
      subsidy_amount: 0,
      emergency_contact_name: d.value.elder_current.emergency_name,
      emergency_contact_phone: d.value.elder_current.emergency_phone,
      family_confirmed_by: ''
    }
  } finally {
    loading.value = false
  }
}

async function doResume() {
  if (!resume.value.address || !resume.value.family_confirmed_by) {
    ElMessage.warning('请填写送餐地址与家属确认人')
    return
  }
  saving.value = true
  try {
    await api.post(`/status-changes/${route.params.id}/resume`, resume.value)
    ElMessage.success('已出院恢复，暂停餐单已结清，剩余次数已重算')
    resumeDlg.value = false
    await load()
  } finally {
    saving.value = false
  }
}

async function doTransfer() {
  if (!transferTo.value || transferItemIds.value.length === 0) {
    ElMessage.warning('请选择餐食与接收老人')
    return
  }
  saving.value = true
  try {
    const r = await api.post(`/status-changes/${route.params.id}/transfer-meal`, {
      item_ids: transferItemIds.value, to_elder_id: transferTo.value
    })
    ElMessage.success(`已转配 ${r.transferred_qty} 份`)
    transferDlg.value = false
    await load()
  } finally {
    saving.value = false
  }
}

async function doRecover() {
  saving.value = true
  try {
    await api.post(`/status-changes/${route.params.id}/recover-boxes`, { count: recoverCount.value })
    ElMessage.success('回收已登记')
    recoverDlg.value = false
    await load()
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  await load()
  try {
    const all = await api.get('/elders')
    activeElders.value = all.filter((e) => e.active && e.service_status === 'active' && e.id !== d.value.elder_id)
  } catch (e) { /* ignore */ }
})
</script>

<style scoped>
.seg-card { border-radius: 10px; padding: 14px 16px; background: #fff; box-shadow: 0 1px 4px rgba(0,0,0,0.06); height: 100%; }
.seg-h { font-weight: 700; font-size: 14px; margin-bottom: 6px; }
.seg-count { font-size: 20px; margin-left: 4px; }
.seg-money { font-size: 16px; font-weight: 700; margin-top: 8px; }
.s1 { border-top: 3px solid #67c23a; }
.s2 { border-top: 3px solid #e6a23c; }
.s4 { border-top: 3px solid #909399; }
</style>
