import { reactive } from 'vue'

export const store = reactive({
  user: JSON.parse(localStorage.getItem('user') || 'null'),
  linkedElders: [],

  get isLoggedIn() {
    return !!localStorage.getItem('token') && !!this.user
  },
  get role() {
    return this.user?.role || ''
  },
  setAuth(token, user) {
    localStorage.setItem('token', token)
    localStorage.setItem('user', JSON.stringify(user))
    this.user = user
  },
  logout() {
    localStorage.removeItem('token')
    localStorage.removeItem('user')
    this.user = null
    this.linkedElders = []
  }
})

export const roleNames = {
  admin: '平台管理员',
  finance: '财政核销',
  community: '社区工作人员',
  kitchen: '厨房',
  rider: '骑手',
  volunteer: '志愿者',
  family: '家属',
  elder: '老人'
}
