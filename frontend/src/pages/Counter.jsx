import React, { useEffect, useState, useCallback } from 'react'
import { api, yuan, TYPE_LABEL, DAMAGE_TYPES, uploadPhoto } from '../api.js'
import { Badge, Modal, Field, Loading, Empty, useToast } from '../components/ui.jsx'
import RentalDetail from '../components/RentalDetail.jsx'

export default function CounterPage({ page, go }) {
  if (page === 'checkout') return <Checkout />
  if (page === 'exchanges') return <Exchanges />
  if (page === 'refits') return <Refits />
  if (page === 'returns') return <Returns />
  return <QueueBoard />
}

const QMETA = [
  ['pickup', '取具队列', 'P'], ['exchange', '换码队列', 'X'],
  ['return_day', '日场归还', 'D'], ['return_night', '夜场归还', 'N'], ['damage', '损坏处理', 'M']
]

function QueueBoard() {
  const [tickets, setTickets] = useState(null)
  const [summary, setSummary] = useState({})
  const [detail, setDetail] = useState(null)
  const toast = useToast()
  const load = useCallback(async () => {
    setTickets(await api('GET', '/counter/queue'))
    setSummary(await api('GET', '/queue-summary'))
  }, [])
  useEffect(() => { load(); const t = setInterval(load, 5000); return () => clearInterval(t) }, [])
  if (!tickets) return <Loading />
  const call = async (type) => {
    try { toast((await api('POST', '/counter/call', { type, station: '1号柜台' })).message) }
    catch (e) { toast(e.message, 'error') }
    load()
  }
  const finish = async (id) => {
    try { await api('POST', `/counter/tickets/${id}/finish`, {}); toast('排队票已结束'); load() }
    catch (e) { toast(e.message, 'error') }
  }

  return (
    <div>
      <div className="btn-row" style={{ marginBottom: 12 }}>
        <h2 style={{ margin: 0, fontSize: 18 }}>柜台排队叫号大屏</h2>
        <span className="spacer" style={{ flex: 1 }} />
        <span className="small muted">每 5 秒自动刷新 · 日场/夜场归还分开排队</span>
      </div>
      <div className="queue-board">
        {QMETA.map(([type, label]) => {
          const list = tickets.filter(t => t.queue_type === type)
          return (
            <div key={type} className={'qcol ' + type}>
              <header><span>{label}</span><span>{summary[type] ?? 0}</span></header>
              <ul>
                {list.length === 0 && <li className="small muted" style={{ border: 0 }}>暂无等待</li>}
                {list.map(t => (
                  <li key={t.id} className={t.status === 'called' ? 'called' : ''} onClick={() => setDetail(t.rental_id)}>
                    <div className="btn-row">
                      <span className="qticket">{t.ticket_no}</span>
                      <span className="spacer" style={{ flex: 1 }} />
                      {t.priority === 1 && <Badge color="purple">团队</Badge>}
                      <Badge color={t.status === 'called' ? 'orange' : 'gray'}>{t.status === 'called' ? '已叫号' : '等待'}</Badge>
                    </div>
                    <div className="small">{t.guest_name} {t.guest_phone}</div>
                    <div className="small muted">{t.course_name || '自由滑'} · {t.created_time}</div>
                    <div className="btn-row" style={{ marginTop: 6 }} onClick={e => e.stopPropagation()}>
                      {t.status === 'waiting' && <button className="btn sm" onClick={() => call(type)}>叫号</button>}
                      <button className="btn sm ghost" onClick={() => finish(t.id)}>结束</button>
                      <button className="btn sm ghost" onClick={() => setDetail(t.rental_id)}>单据</button>
                    </div>
                  </li>
                ))}
              </ul>
            </div>
          )
        })}
      </div>
      {detail && <RentalDetail id={detail} canAssessDamage onClose={() => setDetail(null)} onChanged={load} />}
    </div>
  )
}

// ---------- 扫码出库 ----------
function Checkout() {
  const [qr, setQr] = useState('')
  const [scanned, setScanned] = useState(null)
  const [rentalId, setRentalId] = useState('')
  const [rental, setR] = useState(null)
  const [checked, setChecked] = useState([])
  const toast = useToast()

  const scan = async () => {
    try {
      const d = await api('POST', '/counter/scan', { qr_code: qr.trim() })
      setScanned(d)
      setQr('')
      if (d.rental_id && !checked.includes(d.qr_code)) {
        setChecked(c => [...c, d.qr_code])
        setRentalId(d.rental_id)
        loadRental(d.rental_id)
      } else if (!d.rental_id) {
        toast(`扫到库存雪具：${d.brand} ${d.name} ${d.size_label}（${d.status}）`)
      }
    } catch (e) { toast(e.message, 'error') }
  }
  const loadRental = async (id) => setR(await api('GET', '/rentals/' + id))
  const checkout = async () => {
    try {
      const d = await api('POST', '/counter/checkout', { rental_id: Number(rentalId), qr_codes: checked.length ? checked : undefined })
      toast(d.message + `，冻结押金 ${yuan(d.deposit_hold)}，应还 ${d.return_due_at}`)
      setR(null); setChecked([]); setRentalId(''); setScanned(null)
    } catch (e) { toast(e.message, 'error') }
  }

  return (
    <div>
      <h2 style={{ fontSize: 18 }}>扫码出库</h2>
      <div className="card">
        <div className="row">
          <Field label="扫描/输入雪具二维码">
            <input value={qr} onChange={e => setQr(e.target.value)} placeholder="如 QR-SKI145-001"
              onKeyDown={e => e.key === 'Enter' && qr && scan()} autoFocus />
          </Field>
          <div style={{ alignSelf: 'flex-end' }}><button className="btn" onClick={scan} disabled={!qr}>🔍 扫码核验</button></div>
          <Field label="或直接输入租赁单 ID 整单出库">
            <input value={rentalId} onChange={e => { setRentalId(e.target.value); if (e.target.value) loadRental(e.target.value) }} placeholder="如 1" />
          </Field>
        </div>
        {scanned && <div className="alert info small">
          最近扫码：{scanned.brand} {scanned.name} <b>{scanned.size_label}</b> · {scanned.qr_code}
          {scanned.holder_name ? ` · 当前持有人 ${scanned.holder_name}` : ' · 库存中'}
        </div>}
      </div>

      {rental && <div className="card">
        <div className="btn-row">
          <h3 style={{ margin: 0 }}>租赁单 {rental.code} · {rental.guest_name}</h3>
          <span className="spacer" style={{ flex: 1 }} />
          <Badge color={rental.status === 'reserved' ? 'blue' : 'red'}>{rental.status}</Badge>
        </div>
        <table>
          <thead><tr><th>类型</th><th>建议</th><th>预留雪具</th><th>二维码</th><th>核验</th></tr></thead>
          <tbody>
            {rental.items.map(i => (
              <tr key={i.id}>
                <td>{TYPE_LABEL[i.equipment_type]}</td><td>{i.recommended_size}</td>
                <td>{i.model_label}</td><td className="mono small">{i.qr_code}</td>
                <td>{checked.includes(i.qr_code) ? <Badge color="green">✓ 已扫</Badge> :
                  <Badge color="gray">整单出库时可免扫</Badge>}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <div className="modal-foot">
          <button className="btn" disabled={rental.status !== 'reserved'} onClick={checkout}>
            确认整单出库（冻结押金 {yuan(rental.deposit_total)}）
          </button>
        </div>
      </div>}
    </div>
  )
}

// ---------- 换码处理 ----------
function Exchanges() {
  const [list, setList] = useState(null)
  const [detail, setDetail] = useState(null)
  const toast = useToast()
  const load = async () => setList(await api('GET', '/counter/queue?type=exchange'))
  useEffect(() => { load() }, [])
  return (
    <div>
      <h2 style={{ fontSize: 18 }}>换码处理</h2>
      <p className="muted small">游客中途申请换码进入此队列；可直接扫新雪具二维码，或由系统按期望尺码自动选一件。</p>
      {!list ? <Loading /> : list.length === 0 ? <Empty text="换码队列为空" /> : (
        <div className="grid grid-2">
          {list.map(t => <ExchangeCard key={t.id} ticket={t} onDone={load} onOpen={setDetail} />)}
        </div>
      )}
      {detail && <RentalDetail id={detail} canAssessDamage onClose={() => setDetail(null)} onChanged={load} />}
    </div>
  )
}

function ExchangeCard({ ticket, onDone, onOpen }) {
  const exs = ticket.exchanges || []
  const toast = useToast()
  const process = async (exchangeId, body) => {
    try {
      const d = await api('POST', `/counter/exchanges/${exchangeId}/process`, body)
      toast(d.message)
      onDone()
    } catch (e) { toast(e.message, 'error') }
  }
  return (
    <div className="card">
      <div className="btn-row"><b>{ticket.ticket_no}</b><span className="spacer" style={{ flex: 1 }} />
        <Badge color="purple">{ticket.status === 'called' ? '已叫号' : '等待'}</Badge>{ticket.priority === 1 && <Badge color="purple">团队</Badge>}</div>
      <div style={{ margin: '6px 0' }}>{ticket.guest_name} {ticket.guest_phone} · {ticket.course_name || '自由滑'}</div>
      <div className="small muted">申请时间 {ticket.created_time}</div>
      {exs.length === 0 && <div className="alert warn" style={{ marginTop: 8 }}>该票没有待处理换码申请（可能已处理）</div>}
      {(ticket.refits || []).map(rf => (
        <div key={rf.refit_id} className="alert warn" style={{ marginTop: 8 }}>
          <b>🔁 课前安全重配：{TYPE_LABEL[rf.equipment_type]}（当前 {rf.old_size}）</b>
          <div className="small">原因:{rf.reason} · 请到「课前重配」页按库存推荐换发</div>
        </div>
      ))}
      {exs.map(ex => (
        <div key={ex.exchange_id} className="alert info" style={{ marginTop: 8 }}>
          <b>{TYPE_LABEL[ex.equipment_type]} 换码：{ex.current_label} → {ex.new_size || '同码换一件'}</b>
          <div className="small">原因：{ex.reason}</div>
          <ExchangeActions onProcess={(b) => process(ex.exchange_id, b)} />
        </div>
      ))}
      <div className="modal-foot" style={{ paddingRight: 0 }}>
        <button className="btn sm ghost" onClick={() => onOpen(ticket.rental_id)}>查看单据</button>
        <button className="btn sm" onClick={async () => {
          try { toast((await api('POST', '/counter/call', { type: 'exchange', station: '换码台' })).message); onDone() }
          catch (e) { toast(e.message, 'error') }
        }}>叫号</button>
      </div>
    </div>
  )
}

function ExchangeActions({ onProcess }) {
  const [qr, setQr] = useState('')
  const [showScan, setShowScan] = useState(false)
  const [rejecting, setRejecting] = useState(false)
  const [note, setNote] = useState('')
  return (
    <div style={{ marginTop: 6 }}>
      <div className="btn-row">
        <button className="btn sm" onClick={() => onProcess({ approve: true })}>自动选码换发</button>
        <button className="btn sm ghost" onClick={() => setShowScan(v => !v)}>扫新雪具码换发</button>
        <button className="btn sm danger" onClick={() => setRejecting(v => !v)}>驳回</button>
      </div>
      {showScan && <div className="inline" style={{ marginTop: 6 }}>
        <input placeholder="新雪具二维码 QR-..." value={qr} onChange={e => setQr(e.target.value)} style={{ width: 200 }} />
        <button className="btn sm" onClick={() => onProcess({ approve: true, new_qr_code: qr.trim() })}>确认换发</button>
      </div>}
      {rejecting && <div className="inline" style={{ marginTop: 6 }}>
        <input placeholder="驳回原因" value={note} onChange={e => setNote(e.target.value)} style={{ width: 200 }} />
        <button className="btn sm danger" onClick={() => onProcess({ approve: false, note })}>确认驳回</button>
      </div>}
    </div>
  )
}

// ---------- 课前重配（教练发起的安全重配，按库存推荐替换尺码） ----------
function Refits() {
  const [list, setList] = useState(null)
  const [detail, setDetail] = useState(null)
  const load = useCallback(async () => setList(await api('GET', '/counter/refits')), [])
  useEffect(() => { load(); const t = setInterval(load, 5000); return () => clearInterval(t) }, [])
  return (
    <div>
      <h2 style={{ fontSize: 18 }}>课前尺码重配</h2>
      <p className="muted small">教练课前发现尺码安全隐患（如初学者雪鞋过松）发起的重配。系统按游客体测与当前库存推荐替换尺码；换发免费，游客已获补偿，换发后由教练复核。</p>
      {!list ? <Loading /> : list.length === 0 ? <Empty text="暂无待处理的课前重配" /> : (
        <div className="grid grid-2">
          {list.map(rf => <RefitCard key={rf.id} refit={rf} onDone={load} onOpen={setDetail} />)}
        </div>
      )}
      {detail && <RentalDetail id={detail} onClose={() => setDetail(null)} onChanged={load} />}
    </div>
  )
}

function RefitCard({ refit, onDone, onOpen }) {
  const [rec, setRec] = useState(null)
  const [showScan, setShowScan] = useState(false)
  const [qr, setQr] = useState('')
  const [cancelling, setCancelling] = useState(false)
  const [note, setNote] = useState('')
  const toast = useToast()
  useEffect(() => {
    api('GET', `/counter/refits/${refit.id}/recommend`).then(setRec).catch(() => {})
  }, [refit.id])

  const process = async (body) => {
    try {
      const d = await api('POST', `/counter/refits/${refit.id}/process`, body)
      toast(d.message)
      onDone()
    } catch (e) { toast(e.message, 'error') }
  }
  const top = rec?.candidates?.[0]
  return (
    <div className="card">
      <div className="btn-row"><b>🔁 {TYPE_LABEL[refit.equipment_type]} 课前重配</b>
        <span className="spacer" style={{ flex: 1 }} /><Badge color="orange">待换发</Badge></div>
      <div style={{ margin: '6px 0' }}>{refit.guest_name} {refit.guest_phone} · {refit.course_name || '自由滑'} · 教练 {refit.coach_name}</div>
      <div className="small">原因：<b>{refit.reason}</b>（当前 {refit.old_size}）</div>
      <div className="small muted">课程已顺延 {refit.shift_applied} 分钟（累计 {refit.shift_minutes} 分钟）· 游客补偿 {yuan(refit.compensate_amount)} · 本次免费 · 申请 {refit.created_at}</div>

      {rec && <div className="alert info" style={{ marginTop: 8 }}>
        <b>库存推荐：{rec.target_size}</b>
        <span className="small muted">（{rec.target_reason}）</span>
        <div className="small" style={{ marginTop: 4 }}>
          {(rec.candidates || []).map((cd, i) => (
            <span key={cd.model_id} className="inline" style={{ marginRight: 8 }}>
              {i === 0 ? '⭐ ' : ''}{cd.size_label}（{cd.brand}·库存 {cd.available}）
            </span>
          ))}
          {(!rec.candidates || rec.candidates.length === 0) && <span className="danger-text">该类型暂无可用库存</span>}
        </div>
      </div>}

      <div className="btn-row" style={{ marginTop: 8 }}>
        <button className="btn sm" disabled={!top} onClick={() => process({ approve: true })}>
          按推荐换发{top ? ` ${top.size_label}` : ''}
        </button>
        <button className="btn sm ghost" onClick={() => setShowScan(v => !v)}>扫新雪具码换发</button>
        <button className="btn sm danger" onClick={() => setCancelling(v => !v)}>取消重配</button>
        <span className="spacer" style={{ flex: 1 }} />
        <button className="btn sm ghost" onClick={() => onOpen(refit.rental_id)}>查看单据</button>
      </div>
      {showScan && <div className="inline" style={{ marginTop: 6 }}>
        <input placeholder="新雪具二维码 QR-..." value={qr} onChange={e => setQr(e.target.value)} style={{ width: 200 }} />
        <button className="btn sm" onClick={() => process({ approve: true, new_qr_code: qr.trim() })}>确认换发</button>
      </div>}
      {cancelling && <div className="inline" style={{ marginTop: 6 }}>
        <input placeholder="取消原因（课程顺延与补偿将回滚）" value={note} onChange={e => setNote(e.target.value)} style={{ width: 260 }} />
        <button className="btn sm danger" onClick={() => process({ approve: false, note })}>确认取消</button>
      </div>}
    </div>
  )
}

// ---------- 归还验损 ----------
function Returns() {
  const [qtype, setQtype] = useState('return_day')
  const [list, setList] = useState(null)
  const [rentalId, setRentalId] = useState(null)
  const [rental, setR] = useState(null)
  const toast = useToast()
  const loadQueue = useCallback(async () => setList(await api('GET', `/counter/queue?type=${qtype}`)), [qtype])
  useEffect(() => { loadQueue() }, [qtype])
  const openRental = async (id) => { setRentalId(id); setR(await api('GET', '/rentals/' + id)) }
  const reload = async () => { if (rentalId) setR(await api('GET', '/rentals/' + rentalId)); loadQueue() }

  return (
    <div>
      <h2 style={{ fontSize: 18 }}>归还验损与结算</h2>
      <div className="seg" style={{ marginBottom: 12 }}>
        <button className={qtype === 'return_day' ? 'on' : ''} onClick={() => { setQtype('return_day'); setR(null) }}>☀️ 日场归还队列</button>
        <button className={qtype === 'return_night' ? 'on' : ''} onClick={() => { setQtype('return_night'); setR(null) }}>🌙 夜场归还队列（独立排队）</button>
      </div>

      {!rental && (
        !list ? <Loading /> : list.length === 0 ? <Empty text="该归还队列为空" /> :
          <div className="grid grid-3">
            {list.map(t => (
              <div key={t.id} className="card clickable" onClick={() => openRental(t.rental_id)}>
                <div className="btn-row"><b>{t.ticket_no}</b><span className="spacer" style={{ flex: 1 }} /><Badge color="orange">{t.status === 'called' ? '已叫号' : '等待'}</Badge></div>
                <div>{t.guest_name}</div>
                <div className="small muted">{t.guest_phone} · {t.created_time}</div>
              </div>
            ))}
          </div>
      )}

      {rental && <ReturnInspect rental={rental} onBack={() => { setR(null); setRentalId(null); loadQueue() }} onChanged={reload} />}
    </div>
  )
}

function ReturnInspect({ rental, onBack, onChanged }) {
  const toast = useToast()
  const [target, setTarget] = useState(null)
  const [form, setForm] = useState({ result: 'ok', damage_type: 'edge_damage', severity: 'medium', description: '', photos: [] })
  const [settling, setSettling] = useState(false)
  const [result, setResult] = useState(null)
  const pending = rental.items.filter(i => i.status === 'out')

  const submitItem = async () => {
    try {
      const d = await api('POST', `/counter/returns/${rental.id}/inspect`, {
        rental_item_id: target, ...form
      })
      toast(d.message)
      setTarget(null); onChanged()
    } catch (e) { toast(e.message, 'error') }
  }
  const finish = async () => {
    setSettling(true)
    try {
      const d = await api('POST', `/counter/returns/${rental.id}/finish`, {})
      setResult(d); onChanged()
    } catch (e) { toast(e.message, 'error') } finally { setSettling(false) }
  }
  const onFile = async (f) => {
    try { const url = await uploadPhoto(f); setForm(s => ({ ...s, photos: [...s.photos, url] })); toast('照片已上传') }
    catch (e) { toast(e.message, 'error') }
  }

  return (
    <div className="card">
      <div className="btn-row">
        <button className="btn sm ghost" onClick={onBack}>← 返回队列</button>
        <h3 style={{ margin: 0 }}>{rental.code} · {rental.guest_name}</h3>
        <span className="spacer" style={{ flex: 1 }} />
        {rental.night_return && <Badge color="cyan">夜场归还</Badge>}
      </div>

      {result && <div className="alert info">
        <b>归还结算完成</b>
        <div className="small">使用时长 {result.used_hours} 小时；冻结 {yuan(result.deposit_held)}，
          扣减 {yuan(result.deducted)}（含超时 {yuan(result.late_fee)}），退还 <b className="ok-text">{yuan(result.refund)}</b></div>
        <div className="small muted">{result.insurance_hint}</div>
      </div>}

      <table style={{ marginTop: 10 }}>
        <thead><tr><th>类型</th><th>雪具</th><th>二维码</th><th>状态</th><th>课前确认</th><th></th></tr></thead>
        <tbody>
          {rental.items.map(i => (
            <tr key={i.id}>
              <td>{TYPE_LABEL[i.equipment_type]}</td><td>{i.model_label}</td>
              <td className="mono small">{i.qr_code}</td>
              <td><Badge color={i.status === 'returned_ok' ? 'green' : i.status === 'out' ? 'orange' : i.status === 'repair' || i.status === 'returned_damaged' ? 'red' : 'gray'}>{
                { returned_ok: '✓ 完好', out: '待验', repair: '送修', returned_damaged: '损坏登记', lost: '丢失' }[i.status] || i.status
              }</Badge></td>
              <td className="small">{i.fit_note || (i.fit_status === 'ok' ? '合适合规' : '—')}</td>
              <td>{i.status === 'out' && <button className="btn sm" onClick={() => { setTarget(i.id); setForm({ result: 'ok', damage_type: 'edge_damage', severity: 'medium', description: '', photos: [] }) }}>验具</button>}</td>
            </tr>
          ))}
        </tbody>
      </table>

      {rental.damages?.length > 0 && <div style={{ marginTop: 12 }}>
        <h4>已登记损坏（扣减依据）</h4>
        {rental.damages.map(d => <div key={d.id} className="alert red small">
          {d.type_label} · {d.severity} · 扣 <b>{yuan(d.deduction)}</b> · 状态 {d.status}
          {d.claim_id ? ` · 理赔 #${d.claim_id}` : ''}<div>{d.description}</div>
        </div>)}
      </div>}

      {!result && <div className="modal-foot">
        <span className="small muted">待验雪具 {pending.length} 件</span>
        <span className="spacer" style={{ flex: 1 }} />
        <button className="btn" disabled={pending.length > 0 || settling} onClick={finish}>
          {pending.length > 0 ? `还有 ${pending.length} 件未验` : '完成归还并结算押金'}
        </button>
      </div>}

      {target && <Modal title="单件验损登记" onClose={() => setTarget(null)}>
        <Field label="验具结果">
          <div className="seg">
            <button className={form.result === 'ok' ? 'on' : ''} onClick={() => setForm({ ...form, result: 'ok' })}>✓ 完好上架</button>
            <button className={form.result === 'damaged' ? 'on' : ''} onClick={() => setForm({ ...form, result: 'damaged' })}>损坏</button>
            <button className={form.result === 'lost' ? 'on' : ''} onClick={() => setForm({ ...form, result: 'lost' })}>丢失</button>
          </div>
        </Field>
        {form.result === 'damaged' && <>
          <div className="row">
            <Field label="损坏类型">
              <select value={form.damage_type} onChange={e => setForm({ ...form, damage_type: e.target.value })}>
                {DAMAGE_TYPES.filter(([k]) => k !== 'lost').map(([k, v]) => <option key={k} value={k}>{v}</option>)}
              </select>
            </Field>
            <Field label="严重程度">
              <select value={form.severity} onChange={e => setForm({ ...form, severity: e.target.value })}>
                <option value="light">轻微（扣减×50%）</option>
                <option value="medium">中等（标准比例）</option>
                <option value="heavy">严重（扣减×150%）</option>
              </select>
            </Field>
          </div>
          <Field label="描述"><textarea rows={2} value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} /></Field>
          <Field label="损坏照片"><input type="file" accept="image/*" onChange={e => e.target.files[0] && onFile(e.target.files[0])} /></Field>
          {form.photos.length > 0 && <div className="photos">{form.photos.map((p, i) => <img key={i} src={p} alt="" />)}</div>}
          <p className="small muted">提交后将自动：按押金比例计算扣减 → 生成维修工单 → 有保险自动立案，游客单据可见全部依据。</p>
        </>}
        <div className="modal-foot">
          <button className="btn ghost" onClick={() => setTarget(null)}>取消</button>
          <button className={form.result === 'ok' ? 'btn' : 'btn danger'} onClick={submitItem}>确认验具结果</button>
        </div>
      </Modal>}
    </div>
  )
}
