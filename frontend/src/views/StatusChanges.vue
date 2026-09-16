<template>
  <div>
    <div class="page-card">
      <div class="flex-row" style="justify-content:space-between">
        <h3 class="page-title" style="margin:0">老人状态变更与四段清算</h3>
        <el-button v-if="canOperate" type="primary" @click="openCreate">
          <el-icon style="margin-right:4px"><Plus /></el-icon>登记住院/转院/搬离/去世
        </el-button>
      </div>
      <div class="muted" style="margin:8px 0">
        老人住院、转院、搬离服务区域或去世时，餐单按<b>生效日期</b>拆成
        <el-tag size="small" type="success">已签收</el-tag>
        <el-tag size="small" type="warning">在途</el-tag>
        <el-tag size="small" type="warning">已备餐未出餐</el-tag>
        <el-tag size="small" type="info">未备餐</el-tag>
        四段分别清算；变更须登记生效日期、经办人、家属确认，并同步家属/社区/厨房/财政/骑手。
      </div>
      <div class="flex-row mt-12">
        <el-select v-model="filterType" placeholder="全部类型" clearable style="width:150px" @change="load">
          <el-option v-for="(v,k) in changeTypeMap" :key="k" :label="v.text" :value="k" />
        </el-select>
        <el-select v-model="filterStatus" placeholder="全部状态" clearable style="width:160px" @change="load">
          <el-option v-for="(v,k) in changeStatusMap" :key="k" :label="v.text" :value="k" />
        </el-select>
      </div>
      <el-table :data="list" v-loading="loading" class="mt-12" @row-click="(r)=>$router.push(`/status-changes/${r.id}`)"
        row-style="cursor:pointer">
        <el-table-column prop="id" label="#" width="55" />
        <el-table-column label="老人" width="100">
          <template #default="{ row }">{{ row.elder_name }}</template>
        </el-table-column>
        <el-table-column label="变更类型" width="100">
          <template #default="{ row }">
            <el-tag :type="changeTypeMap[row.change_type]?.type" size="small">{{ changeTypeMap[row.change_type]?.text }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="effective_date" label="生效日期" width="105" />
        <el-table-column label="四段（签/途/备/未）" width="170">
          <template #default="{ row }">
            <el-tag size="small" type="success">{{ row.seg_signed }}</el-tag> /
            <el-tag size="small" type="warning">{{ row.seg_in_transit }}</el-tag> /
            <el-tag size="small" type="warning">{{ row.seg_prepared }}</el-tag> /
            <el-tag size="small" type="info">{{ row.seg_unprepared }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="搬离覆盖" width="120">
          <template #default="{ row }">
            <span v-if="row.change_type==='move_out'">
              <el-tag :type="row.in_coverage?'success':'danger'" size="small">
                {{ row.in_coverage ? '仍在覆盖区' : '超出范围·停配' }}
              </el-tag>
            </span>
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
        <el-table-column label="已签收补贴/损耗" width="150">
          <template #default="{ row }">
            <span>{{ fmtMoney(row.signed_subsidy_total) }}</span>
            <span v-if="Number(row.kitchen_loss_total)>0" class="danger-text"> / 损{{ fmtMoney(row.kitchen_loss_total) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="餐盒回收" width="90">
          <template #default="{ row }">{{ row.boxes_to_recover>0 ? `${row.boxes_recovered}/${row.boxes_to_recover}` : '—' }}</template>
        </el-table-column>
        <el-table-column label="状态" width="110">
          <template #default="{ row }">
            <el-tag :type="changeStatusMap[row.status]?.type" size="small">{{ changeStatusMap[row.status]?.text }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="operator" label="经办人" width="90" />
        <el-table-column label="家属确认" width="110">
          <template #default="{ row }">{{ row.family_confirmed_by || '—' }}</template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 登记向导 -->
    <el-dialog v-model="dlg" :title="`登记状态变更 · ${changeTypeMap[form.change_type]?.text||''}`" width="780px"
      @closed="resetForm">
      <el-steps :active="step" finish-status="success" simple style="margin-bottom:16px">
        <el-step title="选择老人与类型" />
        <el-step title="登记信息与家属确认" />
        <el-step title="四段预览与确认" />
      </el-steps>

      <!-- 步骤1 -->
      <div v-show="step===0">
        <el-form label-width="110px">
          <el-form-item label="老人" required>
            <el-select v-model="form.elder_id" filterable placeholder="选择在服老人" style="width:100%">
              <el-option v-for="e in elders" :key="e.id" :label="`${e.name}（${e.address}）`" :value="e.id" />
            </el-select>
          </el-form-item>
          <el-form-item label="变更类型" required>
            <el-radio-group v-model="form.change_type">
              <el-radio value="hospitalization">住院暂停</el-radio>
              <el-radio value="transfer">转院</el-radio>
              <el-radio value="move_out">搬离</el-radio>
              <el-radio value="death">去世</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="生效日期" required>
            <el-date-picker v-model="form.effective_date" type="date" value-format="YYYY-MM-DD" style="width:200px" />
          </el-form-item>
        </el-form>
      </div>

      <!-- 步骤2 -->
      <div v-show="step===1">
        <el-form label-width="120px">
          <template v-if="form.change_type==='hospitalization' || form.change_type==='transfer'">
            <el-form-item :label="form.change_type==='transfer'?'转入医院':'住院医院'" required>
              <el-input v-model="form.hospital" :placeholder="form.change_type==='transfer'?'转入医院名称':'住院医院名称'" />
            </el-form-item>
            <el-form-item v-if="form.change_type==='transfer'" label="原住院医院">
              <el-input v-model="form.transfer_hospital" placeholder="可选" />
            </el-form-item>
            <el-form-item label="预计出院日">
              <el-date-picker v-model="form.expected_discharge_date" type="date" value-format="YYYY-MM-DD" style="width:200px" />
            </el-form-item>
            <el-form-item label="家属联系人" required>
              <el-input v-model="form.family_contact_name" placeholder="联系人姓名" style="width:160px" />
              <el-input v-model="form.family_contact_phone" placeholder="联系电话" style="width:180px;margin-left:8px" />
            </el-form-item>
            <el-form-item label="补贴资格">
              <el-switch v-model="form.subsidy_retained" active-text="住院期间保留补贴资格" inactive-text="不保留（出院重核）" />
            </el-form-item>
          </template>
          <template v-if="form.change_type==='move_out'">
            <el-form-item label="新地址" required>
              <el-input v-model="form.new_address" placeholder="搬入后的详细地址" @input="checkMoveCoverage" />
            </el-form-item>
            <el-form-item label="覆盖判定">
              <el-tag :type="moveInCoverage?'success':'danger'" size="large">
                {{ moveInCoverage ? '新地址仍在覆盖范围内：继续服务，仅更新送餐地址' : '新地址超出覆盖范围：停止后续配送、回收餐盒保温箱、清算补贴' }}
              </el-tag>
              <div class="muted">覆盖片区：幸福里 / 康乐 / 朝阳社区等辖区范围</div>
            </el-form-item>
            <el-form-item label="家属联系人">
              <el-input v-model="form.family_contact_name" placeholder="家属姓名" style="width:160px" />
              <el-input v-model="form.family_contact_phone" placeholder="电话" style="width:180px;margin-left:8px" />
            </el-form-item>
          </template>
          <template v-if="form.change_type==='death'">
            <el-alert type="info" :closable="false" show-icon
              title="登记去世后：停止配送与补贴核销；已签收记录保留；未出餐部分不进财政结算；已出餐未送达记食材损耗与厨房责任。"
              style="margin-bottom:10px" />
            <el-form-item label="家属联系人">
              <el-input v-model="form.family_contact_name" placeholder="家属姓名" style="width:160px" />
              <el-input v-model="form.family_contact_phone" placeholder="电话" style="width:180px;margin-left:8px" />
            </el-form-item>
          </template>
          <el-form-item label="家属确认人" required>
            <el-input v-model="form.family_confirmed_by" placeholder="确认本次变更的家属姓名（必填）" style="width:260px" />
          </el-form-item>
          <el-form-item label="备注">
            <el-input v-model="form.note" type="textarea" :rows="2" placeholder="经办人备注" />
          </el-form-item>
        </el-form>
      </div>

      <!-- 步骤3 预览 -->
      <div v-show="step===2" v-loading="previewing">
        <el-alert v-if="preview && preview.blocked" type="error" show-icon :closable="false"
          title="存在未办结的补送/改约/退餐/回访，须先按原异常餐单结清，再进入清算" style="margin-bottom:10px">
          <div v-for="a in preview.open_anomalies" :key="a.id" class="danger-text" style="font-size:12px">
            #{{ a.id }} {{ a.order_no }} {{ a.type_name }}：{{ a.description }}
          </div>
        </el-alert>
        <el-alert v-else type="success" show-icon :closable="false"
          title="无未办结异常，可按以下四段直接清算" style="margin-bottom:10px" />

        <el-row :gutter="10" v-if="preview">
          <el-col :span="6"><div class="seg-box seg-1"><div class="seg-n">{{ segCount('signed') }}</div><div class="seg-t">已签收</div><div class="muted">保留·正常结算</div></div></el-col>
          <el-col :span="6"><div class="seg-box seg-2"><div class="seg-n">{{ segCount('in_transit') }}</div><div class="seg-t">在途</div><div class="muted">骑手已取餐·冻结</div></div></el-col>
          <el-col :span="6"><div class="seg-box seg-2"><div class="seg-n">{{ segCount('prepared_undelivered') }}</div><div class="seg-t">已备餐未出餐</div><div class="muted">批次/成本·转配</div></div></el-col>
          <el-col :span="6"><div class="seg-box seg-4"><div class="seg-n">{{ segCount('unprepared') }}</div><div class="seg-t">未备餐</div><div class="muted">取消·释放额度</div></div></el-col>
        </el-row>

        <el-table v-if="preview" :data="previewRows" size="small" max-height="240" class="mt-12">
          <el-table-column prop="order_no" label="单号" width="150" />
          <el-table-column prop="meal_date" label="用餐日期" width="100" />
          <el-table-column label="段" width="150">
            <template #default="{ row }">
              <el-tag :type="segmentMap[row._seg]?.type" size="small">{{ segmentMap[row._seg]?.text }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="batch_no" label="批次" width="130">
            <template #default="{ row }">{{ row.batch_no || '—' }}</template>
          </el-table-column>
          <el-table-column prop="qty" label="份" width="50" />
          <el-table-column label="补贴/成本" width="140">
            <template #default="{ row }">{{ fmtMoney(row.subsidy_amount) }} / {{ fmtMoney(row.material_cost) }}</template>
          </el-table-column>
        </el-table>

        <div v-if="preview && (preview.boxes_to_recover>0 || preview.thermal_boxes_to_recover>0)" class="mt-12">
          <el-alert type="warning" :closable="false" show-icon
            :title="`需回收可循环餐盒 ${preview.boxes_to_recover} 个、在途保温箱 ${preview.thermal_boxes_to_recover} 个`" />
        </div>
      </div>

      <template #footer>
        <el-button @click="dlg=false">取消</el-button>
        <el-button v-if="step>0" @click="step--">上一步</el-button>
        <el-button v-if="step<2" type="primary" @click="nextStep">下一步</el-button>
        <el-button v-if="step===2" type="danger" :disabled="!preview||preview.blocked||submitting" :loading="submitting"
          @click="submit">确认并执行四段清算</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import api from '../api'
import { store } from '../store'
import { changeTypeMap, changeStatusMap, segmentMap, fmtMoney } from '../utils'

const router = useRouter()
const canOperate = computed(() => ['community', 'admin'].includes(store.role))
const list = ref([])
const elders = ref([])
const loading = ref(false)
const filterType = ref('')
const filterStatus = ref('')

const dlg = ref(false)
const step = ref(0)
const previewing = ref(false)
const submitting = ref(false)
const preview = ref(null)
const todayStr = new Date().toISOString().slice(0, 10)
const form = ref({})

function resetForm() {
  form.value = {
    change_type: 'hospitalization', effective_date: todayStr, elder_id: null,
    hospital: '', transfer_hospital: '', expected_discharge_date: '',
    family_contact_name: '', family_contact_phone: '', subsidy_retained: true,
    new_address: '', family_confirmed_by: '', note: ''
  }
  step.value = 0
  preview.value = null
}
function openCreate() {
  resetForm()
  dlg.value = true
}

const moveInCoverage = computed(() => {
  const kw = ['幸福里', '康乐', '朝阳社区', '社区食堂', '辖区']
  if (form.value.change_type !== 'move_out' || !form.value.new_address) return true
  return kw.some((k) => form.value.new_address.includes(k))
})
function checkMoveCoverage() { /* reactive via computed */ }

const previewRows = computed(() => {
  if (!preview.value) return []
  const out = []
  for (const seg of ['signed', 'in_transit', 'prepared_undelivered', 'unprepared']) {
    for (const r of preview.value.segments[seg] || []) out.push({ ...r, _seg: seg })
  }
  return out
})
function segCount(seg) {
  return preview.value ? preview.value.segments.counts[seg] : 0
}

async function nextStep() {
  if (step.value === 0) {
    if (!form.value.elder_id || !form.value.effective_date) {
      ElMessage.warning('请选择老人、变更类型与生效日期')
      return
    }
    step.value = 1
    return
  }
  if (step.value === 1) {
    const f = form.value
    if ((f.change_type === 'hospitalization' || f.change_type === 'transfer') && (!f.hospital || !f.family_contact_name)) {
      ElMessage.warning('住院/转院须登记医院与家属联系人')
      return
    }
    if (f.change_type === 'move_out' && !f.new_address) {
      ElMessage.warning('搬离须填写新地址')
      return
    }
    if (!f.family_confirmed_by) {
      ElMessage.warning('状态变更须登记家属确认人')
      return
    }
    await loadPreview()
    step.value = 2
  }
}

async function loadPreview() {
  previewing.value = true
  try {
    const params = new URLSearchParams({
      type: form.value.change_type,
      effective_date: form.value.effective_date
    })
    if (form.value.change_type === 'move_out') params.set('new_address', form.value.new_address)
    preview.value = await api.get(`/elders/${form.value.elder_id}/status-preview?${params.toString()}`)
  } finally {
    previewing.value = false
  }
}

async function submit() {
  submitting.value = true
  try {
    const res = await api.post(`/elders/${form.value.elder_id}/status-change`, form.value)
    ElMessage.success('四段清算已执行')
    dlg.value = false
    await load()
    router.push(`/status-changes/${res.id}`)
  } finally {
    submitting.value = false
  }
}

async function load() {
  loading.value = true
  try {
    const params = new URLSearchParams()
    if (filterType.value) params.set('type', filterType.value)
    if (filterStatus.value) params.set('status', filterStatus.value)
    const qs = params.toString()
    list.value = await api.get('/status-changes' + (qs ? `?${qs}` : ''))
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  resetForm()
  await load()
  if (canOperate.value) {
    try {
      elders.value = await api.get('/elders')
      elders.value = elders.value.filter((e) => e.active && e.service_status === 'active')
    } catch (e) { /* ignore */ }
  }
})
</script>

<style scoped>
.seg-box {
  border: 1px solid #ebeef5; border-radius: 8px; padding: 12px 8px; text-align: center; background: #fafafa;
}
.seg-n { font-size: 26px; font-weight: 700; }
.seg-t { font-weight: 600; margin: 2px 0; }
.seg-1 .seg-n { color: #67c23a; }
.seg-2 .seg-n { color: #e6a23c; }
.seg-4 .seg-n { color: #909399; }
</style>
