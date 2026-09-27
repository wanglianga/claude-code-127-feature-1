import React, { useEffect, useState } from 'react'
import { api, yuan, TYPE_LABEL, REFIT_STATUS } from '../api.js'
import { Badge, Modal, Field, Loading, useToast } from '../components/ui.jsx'

export default function CoachPage() {
  const [courses, setCourses] = useState(null)
  const [refits, setRefits] = useState([])
  const toast = useToast()
  const load = async () => {
    setCourses(await api('GET', '/coach/courses'))
    setRefits(await api('GET', '/coach/refits'))
  }
  useEffect(() => { load() }, [])

  const check = async (rentalItemId, fitStatus, note = '') => {
    try {
      const d = await api('POST', '/coach/fit-check', { rental_item_id: rentalItemId, fit_status: fitStatus, note })
      toast(d.note ? `${d.message} ${d.note}` : d.message)
      load()
    } catch (e) { toast(e.message, 'error') }
  }

  const confirmRefit = async (id) => {
    try {
      const d = await api('POST', `/coach/refits/${id}/confirm`, { note: '安全动作复核通过，课程时间已重新确认' })
      toast(`${d.message}（${d.course_name} ${d.course_start}）`)
      load()
    } catch (e) { toast(e.message, 'error') }
  }

  if (!courses) return <Loading />
  const pendingConfirm = refits.filter(r => r.status === 'awaiting_confirm')
  const waitingCounter = refits.filter(r => r.status === 'requested')
  return (
    <div>
      <h2 style={{ fontSize: 18 }}>我的课程 · 课前雪具合适合规确认</h2>
      <p className="muted small">上课前逐件确认雪鞋是否合脚、雪板长度是否适合初学者。发现初学者雪鞋过松等安全隐患可直接「发起课前重配」：系统顺延课程开始时间、给游客补偿并通知柜台优先换发。</p>

      {(pendingConfirm.length > 0 || waitingCounter.length > 0) && (
        <div className="card" style={{ borderColor: 'var(--warn, #d97706)' }}>
          <h3 style={{ marginTop: 0 }}>🔁 课前重配跟进</h3>
          {pendingConfirm.map(rf => (
            <div key={rf.id} className="alert warn">
              <div className="btn-row" style={{ justifyContent: 'space-between' }}>
                <b>{rf.guest_name} · {TYPE_LABEL[rf.equipment_type]} {rf.old_size} → {rf.new_size}</b>
                <Badge color="blue">待我复核</Badge>
              </div>
              <div className="small">柜台已换发。请先在下方课前确认中对该件雪具点「✓ 合适」，再复核课程时间（{rf.course_name} 已顺延至 {rf.course_start}）。</div>
              <div className="small muted">{rf.rights_impact}</div>
              <div className="btn-row" style={{ marginTop: 6 }}>
                <button className="btn sm" onClick={() => confirmRefit(rf.id)}>✅ 复核完成：安全动作与课程时间已确认</button>
              </div>
            </div>
          ))}
          {waitingCounter.map(rf => (
            <div key={rf.id} className="alert info small">
              <b>{rf.guest_name} · {TYPE_LABEL[rf.equipment_type]} {rf.old_size}</b> 重配已发起，等待柜台换发
              <span className="muted">（{rf.reason}）</span>
            </div>
          ))}
        </div>
      )}

      {courses.map(co => (
        <div className="card" key={co.id}>
          <div className="btn-row">
            <h3 style={{ margin: 0 }}>{co.name}</h3>
            <span className="spacer" style={{ flex: 1 }} />
            <Badge color={co.session_type === 'night' ? 'gray' : 'blue'}>{co.session_type === 'night' ? '🌙 夜场' : '☀️ 日场'}</Badge>
            <Badge color="cyan">{co.level === 'beginner' ? '初学者' : co.level === 'intermediate' ? '进阶' : '高级'}</Badge>
            {co.shift_minutes > 0 && <Badge color="orange">重配顺延 {co.shift_minutes} 分钟</Badge>}
            <span className="small muted">{co.start_at} ~ {co.end_at}</span>
          </div>
          <div className="table-wrap">
            <table>
              <thead><tr><th>游客</th><th>类型</th><th>建议</th><th>实际</th><th>脚码/身高</th><th>确认状态</th><th>操作</th></tr></thead>
              <tbody>
                {(co.rentals || []).flatMap(rr => rr.items.map(i => (
                  <tr key={i.ri_id}>
                    <td className="nowrap">{rr.guest_name}<div className="small muted mono">{rr.code}</div>
                      {(rr.refits || []).map(rf => <div key={rf.id}><Badge color={REFIT_STATUS[rf.status]?.[1] || 'gray'}>重配 {REFIT_STATUS[rf.status]?.[0] || rf.status}</Badge></div>)}
                    </td>
                    <td>{TYPE_LABEL[i.type]}</td>
                    <td><b>{i.recommended}</b></td>
                    <td>{i.actual}</td>
                    <td className="small">{i.type === 'boot' ? `脚 ${i.guest_foot_eu ? Number(i.guest_foot_eu).toString() : '—'}` : i.type === 'ski' ? `身高 ${i.guest_height_cm || '—'}` : '—'}</td>
                    <td>{i.fit_status === 'ok' ? <Badge color="green">✓ 合适合规</Badge>
                      : i.fit_status === 'issue' ? <Badge color="red">✗ 不合规</Badge>
                      : <Badge color="gray">待确认</Badge>}
                      {i.fit_note && <div className="small muted">{i.fit_note}</div>}</td>
                    <td>
                      <div className="btn-row">
                        <button className="btn sm" onClick={() => check(i.ri_id, 'ok')}>✓ 合适</button>
                        <button className="btn sm danger" onClick={() => check(i.ri_id, 'issue',
                          i.type === 'boot' ? '雪鞋不合脚，建议换半码' : '雪板长度不适合当前水平，建议换板')}>
                          ✗ 不合规
                        </button>
                        <RefitBtn item={i} onDone={load} />
                      </div>
                    </td>
                  </tr>
                )))}
                {(!co.rentals || co.rentals.length === 0) && (
                  <tr><td colSpan={7} className="empty">暂无已出库待确认的学员</td></tr>
                )}
              </tbody>
            </table>
          </div>
        </div>
      ))}
      {courses.length === 0 && <div className="empty">近期没有安排课程</div>}
    </div>
  )
}

// 教练发起课前尺码重配（雪鞋过松等安全隐患）
function RefitBtn({ item, onDone }) {
  const [open, setOpen] = useState(false)
  const [reason, setReason] = useState('')
  const toast = useToast()
  const isBoot = item.type === 'boot'
  const submit = async () => {
    try {
      const d = await api('POST', '/coach/refits', {
        rental_item_id: item.ri_id,
        reason: reason || (isBoot ? '雪鞋过松，滑行时脚跟打滑，影响安全' : '雪具尺码不适配，影响安全')
      })
      toast(d.message)
      setOpen(false); onDone()
    } catch (e) { toast(e.message, 'error') }
  }
  return <>
    <button className="btn sm warn" onClick={() => setOpen(true)}>🔁 课前重配</button>
    {open && <Modal title={`发起课前尺码重配 · ${TYPE_LABEL[item.type]}（当前 ${item.actual || item.recommended}）`} onClose={() => setOpen(false)}>
      <div className="alert warn small">
        适用于开课前发现尺码安全隐患（如初学者雪鞋过松）。发起后系统将：
        <ul style={{ margin: '6px 0 0 16px', padding: 0 }}>
          <li>课程开始时间顺延 15 分钟（单课程累计上限 45 分钟），教练排课同步调整</li>
          <li>游客获得补偿（租金减免 ¥20），本次重配免费</li>
          <li>柜台按库存推荐替换尺码并优先换发，换发后需你重新确认安全动作与课程时间</li>
        </ul>
      </div>
      <Field label="调换原因（游客端可见）">
        <textarea rows={2} value={reason} onChange={e => setReason(e.target.value)}
          placeholder={isBoot ? '雪鞋过松，滑行时脚跟打滑，影响安全' : '雪具尺码不适配，影响安全'} />
      </Field>
      <div className="modal-foot"><button className="btn ghost" onClick={() => setOpen(false)}>取消</button>
        <button className="btn warn" onClick={submit}>确认发起重配</button></div>
    </Modal>}
  </>
}
