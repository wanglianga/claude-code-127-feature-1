import React, { useEffect, useState } from 'react'
import { api, TYPE_LABEL } from '../api.js'
import { Badge, Loading, Modal, Field, useToast } from '../components/ui.jsx'

export default function CoachPage() {
  const [courses, setCourses] = useState(null)
  const toast = useToast()
  const load = async () => setCourses(await api('GET', '/coach/courses'))
  useEffect(() => { load() }, [])

  const check = async (rentalItemId, fitStatus, note = '') => {
    try {
      const d = await api('POST', '/coach/fit-check', { rental_item_id: rentalItemId, fit_status: fitStatus, note })
      toast(d.note ? `${d.message} ${d.note}` : d.message)
      load()
    } catch (e) { toast(e.message, 'error') }
  }

  if (!courses) return <Loading />
  return (
    <div>
      <h2 style={{ fontSize: 18 }}>我的课程 · 课前雪具合适合规确认</h2>
      <p className="muted small">上课前逐件确认雪鞋是否合脚、雪板长度是否适合初学者。发现初学者雪鞋过松影响安全时，可直接发起「课前重配」：柜台按库存推荐尺码换发，耗时影响课程时自动顺延排课并给游客补偿，重配后需在此重新确认安全动作与课程时间。</p>
      {courses.map(co => <CourseCard key={co.id} co={co} onChanged={load} check={check} />)}
      {courses.length === 0 && <div className="empty">近期没有安排课程</div>}
    </div>
  )
}

function CourseCard({ co, onChanged, check }) {
  const toast = useToast()
  const refits = co.refits || []
  return (
    <div className="card">
      <div className="btn-row">
        <h3 style={{ margin: 0 }}>{co.name}</h3>
        <span className="spacer" style={{ flex: 1 }} />
        <Badge color={co.session_type === 'night' ? 'gray' : 'blue'}>{co.session_type === 'night' ? '🌙 夜场' : '☀️ 日场'}</Badge>
        <Badge color="cyan">{co.level === 'beginner' ? '初学者' : co.level === 'intermediate' ? '进阶' : '高级'}</Badge>
        <span className="small muted">{co.start_at} ~ {co.end_at}</span>
      </div>

      {/* 课前重配进度（含课程权益变化，与游客端同步） */}
      {refits.length > 0 && (
        <div style={{ marginTop: 10 }}>
          <h4 style={{ margin: '4px 0' }}>👟 课前重配（{refits.length}）</h4>
          {refits.map(rf => (
            <div key={rf.refit_id} className={'alert ' + refitAlertColor(rf.status)}>
              <div className="btn-row" style={{ justifyContent: 'space-between' }}>
                <b>{rf.guest_name} · {rf.rental_code}：{rf.old_size} → {rf.new_size || rf.target_size}</b>
                <RefitStatusBadge status={rf.status} />
              </div>
              <div className="small" style={{ margin: '4px 0' }}>
                调换原因：{rf.reason}
                {rf.course_start_at && <span> · 原课程 {rf.course_start_at}{rf.course_new_start_at ? ` → 顺延至 ${rf.course_new_start_at}` : ''}</span>}
              </div>
              <div className="small">
                预估 {rf.est_minutes} 分钟{rf.actual_minutes ? `，实际 ${rf.actual_minutes} 分钟` : ''}；
                课程顺延 <b>{rf.delay_minutes}</b> 分钟；游客补偿 <b className="ok-text">¥{Number(rf.compensate_yuan).toFixed(0)}</b>；
                收费 <b>{Number(rf.charge_yuan) === 0 ? '免费（安全重配）' : `¥${rf.charge_yuan}`}</b>
              </div>
              {rf.rights_impact && <div className="small muted">课程权益：{rf.rights_impact}</div>}
              {rf.status === 'done' && (
                <div style={{ marginTop: 8 }}>
                  <div className="small" style={{ color: '#b45309' }}>柜台已换发 {rf.new_size}，请重新确认新雪鞋安全动作与课程时间后学员方可上课。</div>
                  <ReconfirmButton refitId={rf.refit_id} onDone={onChanged} />
                </div>
              )}
              {rf.status === 'confirmed' && rf.coach_reconfirm_note && (
                <div className="small muted">已重新确认：{rf.coach_reconfirm_note}</div>
              )}
            </div>
          ))}
        </div>
      )}

      <div className="table-wrap">
        <table>
          <thead><tr><th>游客</th><th>类型</th><th>建议</th><th>实际</th><th>脚码/身高</th><th>确认状态</th><th>操作</th></tr></thead>
          <tbody>
            {(co.rentals || []).flatMap(rr => rr.items.map(i => (
              <tr key={i.ri_id}>
                <td className="nowrap">{rr.guest_name}<div className="small muted mono">{rr.code}</div></td>
                <td>{TYPE_LABEL[i.type]}</td>
                <td><b>{i.recommended}</b></td>
                <td>{i.actual}</td>
                <td className="small">{i.type === 'boot' ? `脚 ${i.guest_foot_eu ? Number(i.guest_foot_eu).toString() : '—'}` : i.type === 'ski' ? `身高 ${i.guest_height_cm || '—'}` : '—'}</td>
                <td>{fitCell(i)}</td>
                <td>
                  <div className="btn-row">
                    <button className="btn sm" onClick={() => check(i.ri_id, 'ok')}>✓ 合适</button>
                    <button className="btn sm danger" onClick={() => check(i.ri_id, 'issue',
                      i.type === 'boot' ? '雪鞋不合脚，建议换半码' : '雪板长度不适合当前水平，建议换板')}>
                      ✗ 不合规
                    </button>
                    {i.type === 'boot' && <RefitButton rentalItemId={i.ri_id} onDone={onChanged} />}
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
  )
}

function fitCell(i) {
  if (i.fit_status === 'ok') return <Badge color="green">✓ 合适合规</Badge>
  if (i.fit_status === 'issue') return <Badge color="red">✗ 不合规 / 重配中</Badge>
  if (i.fit_status === 'pending' && i.fit_note?.includes('重配')) return <Badge color="orange">重配待确认</Badge>
  return <Badge color="gray">待确认</Badge>
}

function RefitStatusBadge({ status }) {
  if (status === 'requested') return <Badge color="orange">柜台处理中</Badge>
  if (status === 'done') return <Badge color="orange">待教练重新确认</Badge>
  if (status === 'confirmed') return <Badge color="green">✓ 已重新确认</Badge>
  return <Badge color="gray">{status}</Badge>
}
function refitAlertColor(status) {
  if (status === 'confirmed') return 'info'
  return 'warn'
}

// 教练发起课前雪鞋重配
function RefitButton({ rentalItemId, onDone }) {
  const [open, setOpen] = useState(false)
  const [reason, setReason] = useState('雪鞋过松，脚跟打滑影响安全')
  const toast = useToast()
  const submit = async () => {
    try {
      const d = await api('POST', '/coach/refits', { rental_item_id: rentalItemId, reason })
      toast(`${d.message}（排队号 ${d.ticket_no}，目标 ${d.target_size}）`)
      setOpen(false); onDone()
    } catch (e) { toast(e.message, 'error') }
  }
  return <>
    <button className="btn sm warn" onClick={() => setOpen(true)}>👟 发起课前重配</button>
    {open && <Modal title="发起课前雪鞋重配" onClose={() => setOpen(false)}>
      <div className="alert warn small">初学者雪鞋过松会导致脚跟打滑、刹车失控。发起后：① 游客收到安全提醒并入柜台「课前重配」队列；
        ② 柜台按脚码推荐替换尺码（安全重配免费）；③ 耗时若延误课程，课程自动顺延并给游客租金补偿；
        ④ 换发后你需要在本页重新确认安全动作与课程时间。</div>
      <Field label="安全原因">
        <select value={reason} onChange={e => setReason(e.target.value)}>
          <option>雪鞋过松，脚跟打滑影响安全</option>
          <option>雪鞋偏大，转弯时脚在鞋内晃动</option>
          <option>卡扣无法收紧到贴合状态</option>
          <option>初学者反馈脚感不稳，需更换小半码</option>
        </select>
      </Field>
      <div className="modal-foot"><button className="btn ghost" onClick={() => setOpen(false)}>取消</button>
        <button className="btn warn" onClick={submit}>确认发起重配</button></div>
    </Modal>}
  </>
}

// 教练重新确认安全动作与课程时间
function ReconfirmButton({ refitId, onDone }) {
  const [open, setOpen] = useState(false)
  const [safety, setSafety] = useState(false)
  const [schedule, setSchedule] = useState(false)
  const [note, setNote] = useState('')
  const toast = useToast()
  const submit = async () => {
    try {
      const d = await api('POST', `/coach/refits/${refitId}/reconfirm`, {
        safety_confirmed: safety, schedule_confirmed: schedule, note
      })
      toast(d.advice ? `${d.message}（${d.advice}）` : d.message)
      setOpen(false); onDone()
    } catch (e) { toast(e.message, 'error') }
  }
  return <>
    <button className="btn" style={{ marginTop: 8 }} onClick={() => setOpen(true)}>✓ 重新确认安全动作与课程时间</button>
    {open && <Modal title="重配后重新确认" onClose={() => setOpen(false)}>
      <label className="checkbox"><input type="checkbox" checked={safety} onChange={e => setSafety(e.target.checked)} />
        已确认新雪鞋合脚（脚跟不打滑、卡扣贴合），并重新演示安全动作（摔倒姿势 / 犁式刹车 / 安全距离）</label>
      <label className="checkbox" style={{ marginTop: 8 }}><input type="checkbox" checked={schedule} onChange={e => setSchedule(e.target.checked)} />
        已确认顺延后的课程开始时间并通知学员，排课表已同步</label>
      <Field label="确认备注（可选）"><textarea rows={2} value={note} onChange={e => setNote(e.target.value)} placeholder="如：新鞋合脚，安全动作已重新演示确认" /></Field>
      <div className="modal-foot"><button className="btn ghost" onClick={() => setOpen(false)}>取消</button>
        <button className="btn" disabled={!safety || !schedule} onClick={submit}>完成确认，允许上课</button></div>
    </Modal>}
  </>
}
