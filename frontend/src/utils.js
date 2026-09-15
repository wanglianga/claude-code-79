// 状态与字典映射：所有端共用，保证状态展示一致
export const orderStatus = {
  pending: { text: '待确认', type: 'info' },
  confirmed: { text: '已确认·待备餐', type: 'primary' },
  preparing: { text: '厨房备餐中', type: 'warning' },
  ready: { text: '已出餐·待配送', type: 'warning' },
  delivering: { text: '配送中', type: 'warning' },
  signed: { text: '已签收', type: 'success' },
  completed: { text: '已完成（餐盒已回收）', type: 'success' },
  exception: { text: '异常处理中', type: 'danger' },
  cancelled: { text: '已取消', type: 'info' },
  refunded: { text: '已退餐退款', type: 'info' },
  settled: { text: '已核销', type: 'success' }
}

export const mealTypes = { breakfast: '早餐', lunch: '午餐', dinner: '晚餐' }

export const deliveryTypes = {
  home: '配送到家',
  community_pickup: '社区食堂自取',
  volunteer: '志愿者帮送'
}

export const orderSources = { self: '老人自订', family: '家属代订', community: '社区代订' }

export const subsidyLevels = { none: '无补贴', partial: '部分补贴', full: '全额补贴' }

export const boxMethods = {
  next_delivery: '下次送餐回收',
  community_point: '社区回收点',
  onsite: '现场回收'
}

export const anomalyTypes = {
  no_answer: '老人未开门',
  box_not_returned: '餐盒未回收',
  family_change: '家属临时改餐',
  kitchen_shortage: '厨房少做',
  rider_timeout: '骑手超时',
  eligibility_changed: '补贴资格变更',
  meal_unsuitable: '饭菜不适合',
  other: '其他异常'
}

export const anomalyStatus = {
  open: { text: '待处理', type: 'danger' },
  processing: { text: '回访处理中', type: 'warning' },
  resolved: { text: '已办结', type: 'success' }
}

export const boxStatus = {
  pending: { text: '待回收', type: 'danger' },
  partial: { text: '部分回收', type: 'warning' },
  returned: { text: '已回收', type: 'success' }
}

export const deliveryStatus = {
  assigned: { text: '待接单/待取餐', type: 'info' },
  picked: { text: '配送中', type: 'warning' },
  delivered: { text: '已送达', type: 'success' },
  failed: { text: '配送异常', type: 'danger' }
}

export const recStatus = {
  draft: { text: '草稿', type: 'info' },
  confirmed: { text: '已确认', type: 'warning' },
  archived: { text: '已归档（财政档案）', type: 'success' }
}

export const elderStatusMap = {
  fine: { text: '安好', type: 'success' },
  need_help: { text: '需要协助', type: 'warning' },
  urgent: { text: '紧急', type: 'danger' }
}

export function fmtTime(t) {
  if (!t) return '—'
  return String(t).replace('T', ' ').slice(0, 19)
}

export function fmtMoney(n) {
  return '¥' + Number(n || 0).toFixed(2)
}
