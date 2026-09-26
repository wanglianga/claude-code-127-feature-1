import React, { useEffect, useState } from 'react'
import { api, TYPE_LABEL } from '../api.js'
import { Badge, Loading, useToast } from '../components/ui.jsx'

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
      <p className="muted small">上课前逐件确认雪鞋是否合脚、雪板长度是否适合初学者。标记「不合规」会向游客推送安全提醒并入换码统计。</p>
      {courses.map(co => (
        <div className="card" key={co.id}>
          <div className="btn-row">
            <h3 style={{ margin: 0 }}>{co.name}</h3>
            <span className="spacer" style={{ flex: 1 }} />
            <Badge color={co.session_type === 'night' ? 'gray' : 'blue'}>{co.session_type === 'night' ? '🌙 夜场' : '☀️ 日场'}</Badge>
            <Badge color="cyan">{co.level === 'beginner' ? '初学者' : co.level === 'intermediate' ? '进阶' : '高级'}</Badge>
            <span className="small muted">{co.start_at} ~ {co.end_at}</span>
          </div>
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
