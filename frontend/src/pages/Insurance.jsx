import React, { useEffect, useState, useCallback } from 'react'
import { api, yuan, CLAIM_STATUS } from '../api.js'
import { Badge, Modal, Field, Loading, Empty, useToast } from '../components/ui.jsx'

export default function InsurancePage({ page }) {
  if (page === 'policies') return <Policies />
  return <Claims />
}

function Claims() {
  const [list, setList] = useState(null)
  const [target, setTarget] = useState(null)
  const toast = useToast()
  const load = useCallback(async () => setList(await api('GET', '/insurance/claims')), [])
  useEffect(() => { load() }, [])

  const act = async (id, body) => {
    const path = body.status === 'paid' ? 'pay' : 'review'
    try {
      const d = await api('POST', `/insurance/claims/${id}/${path}`, body.status === 'paid' ? {} : body)
      toast(d.message); setTarget(null); load()
    } catch (e) { toast(e.message, 'error') }
  }

  if (!list) return <Loading />
  return (
    <div>
      <h2 style={{ fontSize: 18 }}>保险理赔处理</h2>
      <p className="muted small">损坏评定后自动立案：立案 → 审核 → 核定赔付 → 赔付到账（自动冲抵游客押金扣减）。</p>
      {list.length === 0 && <Empty text="暂无理赔单" />}
      <div className="card" style={{ padding: 0 }}>
        <div className="table-wrap">
          <table>
            <thead><tr><th>#</th><th>游客/单号</th><th>套餐</th><th>损坏</th><th>申请</th><th>免赔</th><th>赔付</th><th>状态</th><th></th></tr></thead>
            <tbody>
              {list.map(c => (
                <tr key={c.id}>
                  <td>{c.id}</td>
                  <td>{c.guest_name}<div className="small muted mono">{c.rental_code}</div></td>
                  <td className="small">{c.product_name}<div className="muted">保费 {yuan(c.premium)}</div></td>
                  <td className="small">{c.damage_label}{c.photos?.length > 0 && ' 📷'}</td>
                  <td>{yuan(c.amount_requested)}</td>
                  <td>{yuan(c.deductible)}</td>
                  <td><b className={c.payout > 0 ? 'ok-text' : ''}>{yuan(c.payout)}</b></td>
                  <td><Badge color={{ filed: 'orange', under_review: 'blue', approved: 'purple', rejected: 'red', paid: 'green' }[c.status]}>{CLAIM_STATUS[c.status]}</Badge>
                    {c.note && <div className="small muted">{c.note}</div>}</td>
                  <td><button className="btn sm ghost" onClick={() => setTarget(c)}>处理</button></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
      {target && <ClaimModal claim={target} onAct={act} onClose={() => setTarget(null)} />}
    </div>
  )
}

function ClaimModal({ claim, onAct, onClose }) {
  const [note, setNote] = useState(claim.note || '')
  const [payout, setPayout] = useState(claim.payout || '')
  return (
    <Modal title={`理赔 #${claim.id} · ${claim.guest_name}`} onClose={onClose}>
      <div className="grid grid-3">
        <div className="stat"><div className="k">申请扣减</div><div className="v">{yuan(claim.amount_requested)}</div></div>
        <div className="stat"><div className="k">免赔额</div><div className="v">{yuan(claim.deductible)}</div></div>
        <div className="stat"><div className="k">保额上限</div><div className="v">见套餐</div></div>
      </div>
      <p className="small muted" style={{ marginTop: 10 }}>损坏：{claim.damage_label}（{claim.severity}）</p>
      <Field label="审核备注"><textarea rows={2} value={note} onChange={e => setNote(e.target.value)} /></Field>
      {(claim.status === 'filed' || claim.status === 'under_review') && <Field label={`核定赔付金额（留空按规则：申请 ${yuan(claim.amount_requested)} − 免赔 ${yuan(claim.deductible)}）`}>
        <input type="number" step="0.01" value={payout} onChange={e => setPayout(e.target.value)} placeholder="自动按免赔/保额计算" />
      </Field>}
      <div className="modal-foot">
        <button className="btn ghost" onClick={onClose}>关闭</button>
        {claim.status === 'filed' && <button className="btn" onClick={() => onAct(claim.id, { status: 'under_review', note })}>进入审核</button>}
        {(claim.status === 'filed' || claim.status === 'under_review') && <>
          <button className="btn danger" onClick={() => onAct(claim.id, { status: 'rejected', note })}>拒赔</button>
          <button className="btn" onClick={() => onAct(claim.id, { status: 'approved', note, payout: payout === '' ? 0 : Number(payout) })}>核定通过</button>
        </>}
        {claim.status === 'approved' && <button className="btn" onClick={() => onAct(claim.id, { status: 'paid' })}>赔付到账并冲抵押金</button>}
        {claim.status === 'paid' && <Badge color="green">已赔付 {yuan(claim.payout)}</Badge>}
      </div>
    </Modal>
  )
}

function Policies() {
  const [list, setList] = useState(null)
  useEffect(() => { api('GET', '/insurance/policies').then(setList) }, [])
  if (!list) return <Loading />
  return (
    <div>
      <h2 style={{ fontSize: 18 }}>保单查询</h2>
      <div className="card" style={{ padding: 0 }}>
        <div className="table-wrap"><table>
          <thead><tr><th>#</th><th>套餐</th><th>游客</th><th>租赁单</th><th>保费</th><th>状态</th><th>投保时间</th></tr></thead>
          <tbody>{list.map(p => <tr key={p.id}>
            <td>{p.id}</td><td>{p.product_name}</td><td>{p.guest_name}</td>
            <td className="mono small">{p.rental_code}</td><td>{yuan(p.premium)}</td>
            <td><Badge color={p.status === 'active' ? 'blue' : p.status === 'claimed' ? 'purple' : 'gray'}>{p.status}</Badge></td>
            <td className="small muted">{String(p.created_at).slice(0, 16)}</td>
          </tr>)}</tbody>
        </table></div>
      </div>
    </div>
  )
}
