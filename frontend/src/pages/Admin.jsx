import React, { useEffect, useState, useCallback } from 'react'
import { api, yuan, TYPE_LABEL, DAMAGE_LABEL } from '../api.js'
import { Badge, Modal, Field, Loading, Empty, useToast } from '../components/ui.jsx'

export default function AdminPage({ page }) {
  if (page === 'trace') return <Trace />
  if (page === 'catalog') return <Catalog />
  return <Analytics />
}

function Analytics() {
  const [d, setD] = useState(null)
  useEffect(() => { api('GET', '/admin/analytics').then(setD).catch(e => setD({ error: e.message })) }, [])
  if (!d) return <Loading />
  if (d.error) return <div className="alert red">分析加载失败：{d.error}</div>

  return (
    <div>
      <h2 style={{ fontSize: 18 }}>雪场运营分析</h2>

      <h3 style={{ marginTop: 18 }}>📉 型号损耗排行（哪些型号损耗高）</h3>
      <div className="card" style={{ padding: 0 }}>
        <div className="table-wrap"><table>
          <thead><tr><th>类型</th><th>品牌/型号/尺码</th><th>库存</th><th>不可租</th><th>损坏次数</th><th>丢失</th><th>累计出租</th><th>损坏率</th><th>维修成本</th></tr></thead>
          <tbody>
            {d.model_wear.map(m => {
              const rate = m.rental_count ? Math.round(m.damage_count * 1000 / m.rental_count) / 10 : 0
              return <tr key={m.id} style={rate >= 20 ? { background: '#fff7ed' } : undefined}>
                <td>{TYPE_LABEL[m.equipment_type]}</td>
                <td>{m.brand} {m.name} <b>{m.size_label}</b></td>
                <td>{m.stock}</td><td>{m.unavailable}</td>
                <td className={m.damage_count > 0 ? 'danger-text' : ''}><b>{m.damage_count}</b></td>
                <td className={m.lost_count > 0 ? 'danger-text' : ''}>{m.lost_count}</td>
                <td>{m.rental_count}</td>
                <td>{rate >= 20 ? <Badge color="orange">{rate}% 高损耗</Badge> : <span className="muted">{rate}%</span>}</td>
                <td>{yuan(m.repair_cost)}</td>
              </tr>
            })}
          </tbody>
        </table></div>
      </div>

      <h3 style={{ marginTop: 18 }}>⏱ 课程时段换码拥堵 & 课前不合规</h3>
      <div className="grid grid-2">
        {d.course_exchange_congestion.map(c => {
          const busy = c.exchange_count >= 2 || c.fit_issue_count >= 2
          return <div className="card" key={c.id} style={busy ? { borderColor: '#fdba74' } : undefined}>
            <div className="btn-row"><b>{c.name}</b><span className="spacer" style={{ flex: 1 }} />
              <Badge color={c.session_type === 'night' ? 'gray' : 'blue'}>{c.session_type === 'night' ? '夜场' : '日场'}</Badge></div>
            <div className="small muted">{c.start_at} · 教练 {c.coach_name || '待定'} · 报名 {c.booked_count}/{c.capacity}</div>
            <div className="btn-row" style={{ marginTop: 8 }}>
              <Badge color={c.exchange_count > 0 ? 'purple' : 'gray'}>换码 {c.exchange_count} 次</Badge>
              <Badge color={c.refit_count > 0 ? 'orange' : 'gray'}>课前重配 {c.refit_count} 次</Badge>
              <Badge color={c.fit_issue_count > 0 ? 'red' : 'gray'}>课前不合规 {c.fit_issue_count} 人</Badge>
              {c.shift_minutes > 0 && <Badge color="orange">重配顺延 {c.shift_minutes} 分钟</Badge>}
              {busy && <Badge color="orange">⚠️ 该时段拥堵，建议增开柜台/调整备货尺码</Badge>}
            </div>
          </div>
        })}
      </div>

      <h3 style={{ marginTop: 18 }}>🎫 旺季分时段取具压力</h3>
      <div className="card" style={{ padding: 0 }}>
        <div className="table-wrap"><table>
          <thead><tr><th>日期</th><th>场次</th><th>时段</th><th>已约/容量</th><th>占用率</th><th></th></tr></thead>
          <tbody>
            {d.slot_pressure.map(s => <tr key={s.id}>
              <td>{s.slot_date}</td>
              <td>{s.session_type === 'night' ? '🌙 夜场' : '☀️ 日场'}</td>
              <td className="mono">{s.start_time}</td>
              <td>{s.booked_count}/{s.capacity}</td>
              <td><div style={{ background: '#f1f5f9', borderRadius: 6, width: 140, height: 10 }}>
                <div style={{ width: Math.min(100, s.occupancy_pct) + '%', height: 10, borderRadius: 6,
                  background: s.occupancy_pct >= 85 ? '#dc2626' : s.occupancy_pct >= 60 ? '#ea580c' : '#16a34a' }} />
              </div></td>
              <td className="small">{s.occupancy_pct}%</td>
            </tr>)}
          </tbody>
        </table></div>
      </div>

      <h3 style={{ marginTop: 18 }}>⚠️ 需额外安全提醒的游客</h3>
      {d.guest_safety.length === 0 ? <Empty text="暂无高风险游客" /> : (
        <div className="grid grid-2">
          {d.guest_safety.map(g => <div className="card" key={g.id}>
            <div className="btn-row"><b>{g.name}</b><span className="spacer" style={{ flex: 1 }} />
              <Badge color="orange">提醒 {g.reminder_count} 次</Badge>
              <Badge color="red">不合规 {g.fit_issue_count} 次</Badge>
              <Badge color="purple">换码 {g.exchange_count} 次</Badge></div>
            <div className="small muted">{g.phone} · 水平：{g.level === 'beginner' ? '初学者' : g.level}</div>
            {g.latest_messages && <div className="alert warn small" style={{ marginTop: 6 }}>{g.latest_messages}</div>}
          </div>)}
        </div>
      )}

      <h3 style={{ marginTop: 18 }}>🔧 损坏类型分布</h3>
      <div className="btn-row">
        {d.damage_types.map(x => <Badge key={x.damage_type} color="red">{x.label} × {x.n}</Badge>)}
      </div>

      <h3 style={{ marginTop: 18 }}>🔁 课前尺码重配记录（教练发起的安全重配）</h3>
      {(d.precourse_refits || []).length === 0 ? <Empty text="暂无课前重配" /> : (
        <div className="card" style={{ padding: 0 }}>
          <div className="table-wrap"><table>
            <thead><tr><th>时间</th><th>课程</th><th>游客</th><th>雪具</th><th>尺码变化</th><th>原因</th><th>补偿</th><th>顺延</th><th>状态</th></tr></thead>
            <tbody>
              {d.precourse_refits.map(rf => <tr key={rf.id}>
                <td className="small nowrap">{rf.created_at}</td>
                <td className="small">{rf.course_name || '自由滑'}</td>
                <td>{rf.guest_name}</td>
                <td>{TYPE_LABEL[rf.equipment_type]}</td>
                <td className="small">{rf.old_size}{rf.new_size ? ` → ${rf.new_size}` : ''}</td>
                <td className="small muted">{rf.reason}</td>
                <td className="ok-text">{yuan(rf.compensate_amount)}</td>
                <td>{rf.shift_applied} 分钟</td>
                <td>{rf.status === 'done' ? <Badge color="green">已完成</Badge>
                  : rf.status === 'awaiting_confirm' ? <Badge color="blue">待教练复核</Badge>
                  : rf.status === 'cancelled' ? <Badge color="gray">已取消</Badge>
                  : <Badge color="orange">待柜台换发</Badge>}</td>
              </tr>)}
            </tbody>
          </table></div>
        </div>
      )}
    </div>
  )
}

function Trace() {
  const [status, setStatus] = useState('')
  const [list, setList] = useState(null)
  const load = useCallback(async () => {
    setList(await api('GET', '/admin/equipment-trace' + (status ? '?status=' + status : '')))
  }, [status])
  useEffect(() => { load() }, [status])
  const STATUSES = [['', '全部'], ['available', '可出租'], ['reserved', '已预约'], ['out', '在游客手中'], ['inspection', '检查中'], ['repair', '维修中'], ['retired', '已报废']]
  return (
    <div>
      <div className="btn-row">
        <h2 style={{ margin: 0, fontSize: 18 }}>雪具全程追踪</h2>
        <span className="spacer" style={{ flex: 1 }} />
        <div className="seg">{STATUSES.map(([k, v]) =>
          <button key={k} className={status === k ? 'on' : ''} onClick={() => setStatus(k)}>{v}</button>)}</div>
      </div>
      <p className="muted small">每套雪具现在在谁手里、是否关联课程、是否适配（课前确认）、是否可继续出租，一屏可查。</p>
      {!list ? <Loading /> : <div className="card" style={{ padding: 0 }}>
        <div className="table-wrap"><table>
          <thead><tr><th>二维码</th><th>类型</th><th>品牌型号尺码</th><th>状态</th><th>持有人</th><th>课程/场次</th><th>课前适配</th><th>累计出租</th></tr></thead>
          <tbody>
            {list.map(i => <tr key={i.id}>
              <td className="mono small">{i.qr_code}</td>
              <td>{TYPE_LABEL[i.equipment_type]}</td>
              <td>{i.brand} {i.name} {i.size_label}</td>
              <td><Badge color={i.rentable ? 'green' : i.status === 'out' ? 'blue' : i.status === 'repair' ? 'red' : 'gray'}>{i.status_label}</Badge></td>
              <td>{i.holder_name ? <>{i.holder_name}<div className="small muted">{i.holder_phone}</div></> : <span className="muted">—</span>}</td>
              <td className="small">{i.course_name || '—'}{i.session_type === 'night' ? ' 🌙' : ''}</td>
              <td>{i.fit_status === 'ok' ? <Badge color="green">适配</Badge> : i.fit_status === 'issue' ? <Badge color="red">不适配</Badge> : '—'}</td>
              <td>{i.rental_count}</td>
            </tr>)}
          </tbody>
        </table></div>
      </div>}
    </div>
  )
}

function Catalog() {
  const [models, setModels] = useState(null)
  const [tab, setTab] = useState('model')
  const toast = useToast()
  const load = async () => setModels(await api('GET', '/models'))
  useEffect(() => { load() }, [])

  return (
    <div>
      <div className="btn-row">
        <h2 style={{ margin: 0, fontSize: 18 }}>雪具型号与库存入库</h2>
        <span className="spacer" style={{ flex: 1 }} />
        <div className="seg">
          <button className={tab === 'model' ? 'on' : ''} onClick={() => setTab('model')}>新型号</button>
          <button className={tab === 'item' ? 'on' : ''} onClick={() => setTab('item')}>单品入库（生成二维码）</button>
        </div>
      </div>
      {tab === 'model' ? <ModelForm onDone={load} /> : <ItemForm models={models} onDone={load} toast={toast} />}
      {!models ? <Loading /> : <div className="card" style={{ padding: 0, marginTop: 16 }}>
        <div className="table-wrap"><table>
          <thead><tr><th>编码</th><th>类型</th><th>品牌/名称</th><th>尺码</th><th>租金</th><th>押金</th><th>库存/可用</th></tr></thead>
          <tbody>
            {models.map(m => <tr key={m.id}>
              <td className="mono small">{m.code}</td><td>{TYPE_LABEL[m.type]}</td>
              <td>{m.brand} {m.name}</td><td><b>{m.size_label}</b></td>
              <td>{yuan(m.rental_fee)}</td><td>{yuan(m.deposit)}</td>
              <td>{m.total} / <span className={m.available > 0 ? 'ok-text' : 'danger-text'}>{m.available}</span></td>
            </tr>)}
          </tbody>
        </table></div>
      </div>}
    </div>
  )
}

function ModelForm({ onDone }) {
  const toast = useToast()
  const [f, setF] = useState({ code: '', brand: '', name: '', type: 'ski', size_label: '', size_value: '', rental_fee: '', deposit: '' })
  const submit = async () => {
    try {
      await api('POST', '/admin/models', { ...f, size_value: Number(f.size_value), rental_fee: Number(f.rental_fee), deposit: Number(f.deposit) })
      toast('型号已创建'); setF({ ...f, code: '', size_label: '', size_value: '', rental_fee: '', deposit: '' }); onDone()
    } catch (e) { toast(e.message, 'error') }
  }
  return <div className="card">
    <div className="row">
      <Field label="型号编码（唯一）"><input value={f.code} onChange={e => setF({ ...f, code: e.target.value })} placeholder="如 SKI150" /></Field>
      <Field label="类型"><select value={f.type} onChange={e => setF({ ...f, type: e.target.value })}>
        {Object.entries(TYPE_LABEL).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
      </select></Field>
      <Field label="品牌"><input value={f.brand} onChange={e => setF({ ...f, brand: e.target.value })} placeholder="Atomic" /></Field>
      <Field label="名称"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} placeholder="全地域雪板" /></Field>
    </div>
    <div className="row">
      <Field label="尺码标签（145cm / EU42 / M）"><input value={f.size_label} onChange={e => setF({ ...f, size_label: e.target.value })} /></Field>
      <Field label="尺码数值（用于自动匹配：板长cm/欧码/码序）"><input type="number" step="0.5" value={f.size_value} onChange={e => setF({ ...f, size_value: e.target.value })} /></Field>
      <Field label="日租金（元）"><input type="number" value={f.rental_fee} onChange={e => setF({ ...f, rental_fee: e.target.value })} /></Field>
      <Field label="单件押金（元）"><input type="number" value={f.deposit} onChange={e => setF({ ...f, deposit: e.target.value })} /></Field>
    </div>
    <div className="modal-foot" style={{ paddingRight: 0 }}><button className="btn" onClick={submit}>创建型号</button></div>
  </div>
}

function ItemForm({ models, onDone, toast }) {
  const [modelId, setModelId] = useState('')
  const [serial, setSerial] = useState('')
  const [qty, setQty] = useState(1)
  const submit = async () => {
    try {
      const d = await api('POST', '/admin/items', { model_id: Number(modelId), serial_no: serial || 'SN', qty: Number(qty) })
      toast(d.message); setSerial(''); setQty(1); onDone()
    } catch (e) { toast(e.message, 'error') }
  }
  return <div className="card">
    <div className="row">
      <Field label="选择型号">
        <select value={modelId} onChange={e => setModelId(e.target.value)}>
          <option value="">请选择</option>
          {(models || []).map(m => <option key={m.id} value={m.id}>{TYPE_LABEL[m.type]} · {m.brand} {m.name} {m.size_label}（可用 {m.available}）</option>)}
        </select>
      </Field>
      <Field label="序列号前缀（可选）"><input value={serial} onChange={e => setSerial(e.target.value)} placeholder="SN-2026" /></Field>
      <Field label="入库数量"><input type="number" min="1" value={qty} onChange={e => setQty(e.target.value)} /></Field>
    </div>
    <p className="small muted">入库后每件雪具自动生成唯一二维码（QR-型号编码-序号），柜台扫码出库/验损。</p>
    <div className="modal-foot" style={{ paddingRight: 0 }}><button className="btn" disabled={!modelId} onClick={submit}>确认入库</button></div>
  </div>
}
